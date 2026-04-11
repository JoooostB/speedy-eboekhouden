package eboekhouden

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const soapEndpoint = "https://soap.e-boekhouden.nl/soap.asmx"
const soapSessionTTL = 20 * time.Minute

var soapHTTPClient = &http.Client{Timeout: 30 * time.Second}

// SoapClient provides access to the e-boekhouden SOAP API.
type SoapClient struct {
	username      string
	securityCode1 string
	securityCode2 string
	source        string

	sessionID   string
	sessionTime time.Time
	mu          sync.Mutex
}

// NewSoapClient creates a new SOAP API client.
func NewSoapClient(username, securityCode1, securityCode2 string) *SoapClient {
	return &SoapClient{
		username:      username,
		securityCode1: securityCode1,
		securityCode2: securityCode2,
		source:        "Speedy",
	}
}

func (c *SoapClient) ensureSession() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sessionID != "" && time.Since(c.sessionTime) < soapSessionTTL {
		return c.sessionID, nil
	}

	body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:ebh="http://www.e-boekhouden.nl/soap">
  <soap:Body>
    <ebh:OpenSession>
      <ebh:Username>%s</ebh:Username>
      <ebh:SecurityCode1>%s</ebh:SecurityCode1>
      <ebh:SecurityCode2>%s</ebh:SecurityCode2>
      <ebh:Source>%s</ebh:Source>
    </ebh:OpenSession>
  </soap:Body>
</soap:Envelope>`, xmlEscape(c.username), xmlEscape(c.securityCode1), xmlEscape(c.securityCode2), xmlEscape(c.source))

	respBody, err := c.doSOAP("OpenSession", body)
	if err != nil {
		return "", fmt.Errorf("opening SOAP session: %w", err)
	}

	sessionID := extractXMLValue(respBody, "SessionID")
	if sessionID == "" {
		errMsg := extractXMLValue(respBody, "ErrorMsg")
		if errMsg != "" {
			return "", fmt.Errorf("SOAP session error: %s", errMsg)
		}
		return "", fmt.Errorf("no session ID in SOAP response")
	}

	c.sessionID = sessionID
	c.sessionTime = time.Now()
	return sessionID, nil
}

func (c *SoapClient) call(method string, innerXML string) (string, error) {
	sessionID, err := c.ensureSession()
	if err != nil {
		return "", err
	}

	body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:ebh="http://www.e-boekhouden.nl/soap">
  <soap:Body>
    <ebh:%s>
      <ebh:SessionID>%s</ebh:SessionID>
      <ebh:SecurityCode2>%s</ebh:SecurityCode2>
      %s
    </ebh:%s>
  </soap:Body>
</soap:Envelope>`, method, xmlEscape(sessionID), xmlEscape(c.securityCode2), innerXML, method)

	respBody, err := c.doSOAP(method, body)
	if err != nil {
		return "", err
	}

	errMsg := extractXMLValue(respBody, "ErrorMsg")
	if errMsg != "" {
		return "", fmt.Errorf("SOAP %s error: %s", method, errMsg)
	}

	return respBody, nil
}

func (c *SoapClient) doSOAP(action, body string) (string, error) {
	req, err := http.NewRequest("POST", soapEndpoint, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", "http://www.e-boekhouden.nl/soap/"+action)

	resp, err := soapHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("SOAP request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB max
	if err != nil {
		return "", fmt.Errorf("reading SOAP response: %w", err)
	}

	return string(data), nil
}

// GetRelaties returns all relations matching the filter.
func (c *SoapClient) GetRelaties(filter string) (json.RawMessage, error) {
	filterXML := "<ebh:cFilter>"
	if filter != "" {
		filterXML += "<ebh:Trefwoord>" + xmlEscape(filter) + "</ebh:Trefwoord>"
	}
	filterXML += "</ebh:cFilter>"

	resp, err := c.call("GetRelaties", filterXML)
	if err != nil {
		return nil, err
	}
	return soapResultToJSON(resp, "Relaties", "cRelatie")
}

// GetGrootboekrekeningen returns all ledger accounts.
func (c *SoapClient) GetGrootboekrekeningen() (json.RawMessage, error) {
	resp, err := c.call("GetGrootboekrekeningen", "<ebh:cFilter></ebh:cFilter>")
	if err != nil {
		return nil, err
	}
	return soapResultToJSON(resp, "Rekeningen", "cGrootboekrekening")
}

// GetSaldi returns balances for the given date range and ledger category.
func (c *SoapClient) GetSaldi(datumVan, datumTot string, kostenplaatsID int) (json.RawMessage, error) {
	filterXML := fmt.Sprintf(`<ebh:cFilter>
    <ebh:DatumVan>%s</ebh:DatumVan>
    <ebh:DatumTot>%s</ebh:DatumTot>
    <ebh:KostenplaatsID>%d</ebh:KostenplaatsID>
  </ebh:cFilter>`, xmlEscape(datumVan), xmlEscape(datumTot), kostenplaatsID)

	resp, err := c.call("GetSaldi", filterXML)
	if err != nil {
		return nil, err
	}
	return soapResultToJSON(resp, "Saldi", "cSaldo")
}

// GetOpenPosten returns open items (debiteuren or crediteuren).
func (c *SoapClient) GetOpenPosten(soort string) (json.RawMessage, error) {
	resp, err := c.call("GetOpenPosten", "<ebh:OpSoort>"+xmlEscape(soort)+"</ebh:OpSoort>")
	if err != nil {
		return nil, err
	}
	return soapResultToJSON(resp, "OpenPosten", "cOpenPost")
}

// GetMutaties returns mutations matching the filter.
func (c *SoapClient) GetMutaties(datumVan, datumTot string, mutatieNr int) (json.RawMessage, error) {
	filterXML := "<ebh:cFilter>"
	if datumVan != "" {
		filterXML += "<ebh:DatumVan>" + xmlEscape(datumVan) + "</ebh:DatumVan>"
	}
	if datumTot != "" {
		filterXML += "<ebh:DatumTot>" + xmlEscape(datumTot) + "</ebh:DatumTot>"
	}
	if mutatieNr > 0 {
		filterXML += fmt.Sprintf("<ebh:MutatieNr>%d</ebh:MutatieNr>", mutatieNr)
	}
	filterXML += "</ebh:cFilter>"

	resp, err := c.call("GetMutaties", filterXML)
	if err != nil {
		return nil, err
	}
	return soapResultToJSON(resp, "Mutaties", "cMutatie")
}

// GetArtikelen returns all articles.
func (c *SoapClient) GetArtikelen() (json.RawMessage, error) {
	resp, err := c.call("GetArtikelen", "<ebh:cFilter></ebh:cFilter>")
	if err != nil {
		return nil, err
	}
	return soapResultToJSON(resp, "Artikelen", "cArtikel")
}

// GetKostenplaatsen returns cost centers.
func (c *SoapClient) GetKostenplaatsen() (json.RawMessage, error) {
	resp, err := c.call("GetKostenplaatsen", "<ebh:cFilter></ebh:cFilter>")
	if err != nil {
		return nil, err
	}
	return soapResultToJSON(resp, "Kostenplaatsen", "cKostenplaats")
}

// MutatieInput holds the fields needed to create a mutation via SOAP.
// All string fields are XML-escaped before interpolation.
type MutatieInput struct {
	Soort             string
	Datum             string
	Rekening          string
	RelatieCode       string
	Factuurnummer     string
	Boekstuk          string
	Omschrijving      string
	InExBTW           string
	BedragInvoer      string
	BedragExclBTW     string
	BedragBTW         string
	BedragInclBTW     string
	BTWCode           string
	TegenrekeningCode string
}

// AddMutatie creates a new mutation via SOAP with escaped fields.
func (c *SoapClient) AddMutatie(m MutatieInput) (json.RawMessage, error) {
	innerXML := fmt.Sprintf(`<ebh:oMut>
    <ebh:Soort>%s</ebh:Soort>
    <ebh:Datum>%s</ebh:Datum>
    <ebh:Rekening>%s</ebh:Rekening>
    <ebh:RelatieCode>%s</ebh:RelatieCode>
    <ebh:Factuurnummer>%s</ebh:Factuurnummer>
    <ebh:Boekstuk>%s</ebh:Boekstuk>
    <ebh:Omschrijving>%s</ebh:Omschrijving>
    <ebh:InExBTW>%s</ebh:InExBTW>
    <ebh:MutatieRegels>
      <ebh:cMutatieRegel>
        <ebh:BedragInvoer>%s</ebh:BedragInvoer>
        <ebh:BedragExclBTW>%s</ebh:BedragExclBTW>
        <ebh:BedragBTW>%s</ebh:BedragBTW>
        <ebh:BedragInclBTW>%s</ebh:BedragInclBTW>
        <ebh:BTWCode>%s</ebh:BTWCode>
        <ebh:TegenrekeningCode>%s</ebh:TegenrekeningCode>
      </ebh:cMutatieRegel>
    </ebh:MutatieRegels>
  </ebh:oMut>`,
		xmlEscape(m.Soort), xmlEscape(m.Datum), xmlEscape(m.Rekening),
		xmlEscape(m.RelatieCode), xmlEscape(m.Factuurnummer), xmlEscape(m.Boekstuk),
		xmlEscape(m.Omschrijving), xmlEscape(m.InExBTW),
		xmlEscape(m.BedragInvoer), xmlEscape(m.BedragExclBTW),
		xmlEscape(m.BedragBTW), xmlEscape(m.BedragInclBTW),
		xmlEscape(m.BTWCode), xmlEscape(m.TegenrekeningCode))

	resp, err := c.call("AddMutatie", innerXML)
	if err != nil {
		return nil, err
	}

	mutNr := extractXMLValue(resp, "MutatieNr")
	return json.Marshal(map[string]string{"mutatieNr": mutNr})
}

// Helper: extract a simple XML value by tag name
func extractXMLValue(xmlStr, tagName string) string {
	start := strings.Index(xmlStr, "<"+tagName+">")
	if start == -1 {
		// Try with namespace prefix
		for _, prefix := range []string{"a:", "b:", ""} {
			start = strings.Index(xmlStr, "<"+prefix+tagName+">")
			if start != -1 {
				tagName = prefix + tagName
				break
			}
		}
		if start == -1 {
			return ""
		}
	}
	start += len("<" + tagName + ">")
	end := strings.Index(xmlStr[start:], "</"+tagName+">")
	if end == -1 {
		return ""
	}
	return xmlStr[start : start+end]
}

// soapResultToJSON extracts the array wrapper from SOAP XML and converts to JSON.
func soapResultToJSON(xmlStr, wrapperTag, itemTag string) (json.RawMessage, error) {
	// Find the wrapper content
	content := extractXMLBlock(xmlStr, wrapperTag)
	if content == "" {
		return json.Marshal([]any{})
	}

	// Parse individual items
	var items []map[string]any
	remaining := content
	for {
		item := extractXMLBlock(remaining, itemTag)
		if item == "" {
			break
		}
		m := xmlToMap(item)
		if len(m) > 0 {
			items = append(items, m)
		}
		idx := strings.Index(remaining, "</"+itemTag+">")
		if idx == -1 {
			// Try with namespace
			for _, p := range []string{"a:", "b:"} {
				idx = strings.Index(remaining, "</"+p+itemTag+">")
				if idx != -1 {
					idx += len("</" + p + itemTag + ">")
					break
				}
			}
			if idx == -1 {
				break
			}
		} else {
			idx += len("</" + itemTag + ">")
		}
		remaining = remaining[idx:]
	}

	if items == nil {
		items = []map[string]any{}
	}
	return json.Marshal(items)
}

func extractXMLBlock(xmlStr, tag string) string {
	for _, prefix := range []string{"", "a:", "b:"} {
		fullTag := prefix + tag
		start := strings.Index(xmlStr, "<"+fullTag+">")
		if start == -1 {
			start = strings.Index(xmlStr, "<"+fullTag+" ")
		}
		if start == -1 {
			continue
		}
		// Find the actual start of content after the opening tag
		contentStart := strings.Index(xmlStr[start:], ">")
		if contentStart == -1 {
			continue
		}
		contentStart = start + contentStart + 1
		end := strings.Index(xmlStr[contentStart:], "</"+fullTag+">")
		if end == -1 {
			continue
		}
		return xmlStr[contentStart : contentStart+end]
	}
	return ""
}

func xmlToMap(xmlStr string) map[string]any {
	decoder := xml.NewDecoder(strings.NewReader("<root>" + xmlStr + "</root>"))
	m := make(map[string]any)
	var currentKey string
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			currentKey = lcFirst(t.Name.Local)
		case xml.CharData:
			if currentKey != "" && currentKey != "root" {
				val := strings.TrimSpace(string(t))
				if val != "" {
					m[currentKey] = tryParseNumber(val)
				}
			}
		case xml.EndElement:
			currentKey = ""
		}
	}
	return m
}

// lcFirst lowercases the first character of a string (PascalCase → camelCase).
func lcFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// tryParseNumber attempts to parse a string as a number. Returns the original string if not numeric.
func tryParseNumber(s string) any {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		// Only convert if it looks like a number (not an ID-like string that happens to be numeric)
		if strings.Contains(s, ".") || strings.Contains(s, ",") || (len(s) < 15 && !strings.HasPrefix(s, "0")) {
			return f
		}
	}
	return s
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s))
	return buf.String()
}
