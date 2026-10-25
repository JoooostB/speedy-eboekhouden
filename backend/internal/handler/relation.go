package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/eboekhouden"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// relationSummary is the part of an e-boekhouden relation the invoice review
// needs. GrootboekrekeningID is the relation's default ledger account
// (internal id, the same id space as actieveGBR); the review dialog prefers
// it over Claude's grootboekcode. 0 means the relation has none, nil (JSON
// null) that it is unknown.
type relationSummary struct {
	ID                  int    `json:"id"`
	Code                string `json:"code"`
	Bedrijf             string `json:"bedrijf"`
	GrootboekrekeningID *int   `json:"grootboekrekeningId"`
}

// matchedRelationFromSearch picks the first hit of a relation search as the
// supplier relation, or nil when nothing matched. Whether a search hit carries
// grootboekrekeningId is not established; without it the field stays nil.
func matchedRelationFromSearch(raw json.RawMessage) *relationSummary {
	var relations []relationSummary
	if json.Unmarshal(raw, &relations) != nil || len(relations) == 0 || relations[0].ID == 0 {
		return nil
	}
	return &relations[0]
}

// parseRelationModel reads GET /v1/api/relatie/{id}. The model always has
// grootboekrekeningId; a missing one is read as "none".
func parseRelationModel(raw json.RawMessage) (relationSummary, error) {
	var r relationSummary
	if err := json.Unmarshal(raw, &r); err != nil {
		return relationSummary{}, err
	}
	if r.GrootboekrekeningID == nil {
		zero := 0
		r.GrootboekrekeningID = &zero
	}
	return r, nil
}

// fillRelationDefaultLedger looks up the relation's default ledger account
// when the search hit did not carry it. Best effort: on any error the field
// stays nil and the dialog falls back to Claude's suggestion. The error is
// returned so callers can pass ErrSessionExpired on.
func fillRelationDefaultLedger(client *eboekhouden.Client, r *relationSummary) error {
	raw, err := client.GetRelation(r.ID)
	if err != nil {
		log.Printf("Relation default ledger lookup failed for relation %d: %s", r.ID, errClass(err))
		return err
	}
	full, err := parseRelationModel(raw)
	if err != nil {
		log.Printf("Relation default ledger lookup for relation %d: unreadable model", r.ID)
		return err
	}
	r.GrootboekrekeningID = full.GrootboekrekeningID
	return nil
}

// GetRelation handles GET /api/v1/relations/:id. It answers the summary only
// (id, code, bedrijf, grootboekrekeningId), not the full model with address
// and contact details, so the review dialog can apply the default ledger
// account of a relation picked from the search.
func (h *InvoiceHandler) GetRelation(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ongeldige relatie."})
		return
	}
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}
	raw, err := client.GetRelation(id)
	if err != nil {
		if HandleEBoekhoudenSessionExpired(c, session.FromContext(c), h.sessions, h.db, err) {
			return
		}
		log.Printf("GetRelation %d: %s", id, errClass(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "Relatie ophalen bij e-Boekhouden mislukt."})
		return
	}
	r, err := parseRelationModel(raw)
	if err != nil || r.ID != id {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Relatie ophalen bij e-Boekhouden mislukt."})
		return
	}
	c.JSON(http.StatusOK, r)
}
