package eboekhouden

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// FolderHeader is the response of GET /v1/api/folder/{id}/header. The
// administration-wide fields (the "Mail & herken" folder id, credits and data
// usage) are identical for every folder id; DirectoryInfo describes the
// requested folder.
type FolderHeader struct {
	MenhFolderID  int                 `json:"menhFolderId"`
	Credits       int                 `json:"credits"`
	DataLimit     int64               `json:"dataLimit"`
	DataUsed      int64               `json:"dataUsed"`
	DirectoryInfo FolderDirectoryInfo `json:"directoryInfo"`
}

// FolderDirectoryInfo describes one archive folder.
type FolderDirectoryInfo struct {
	DirectoryName     string `json:"directoryName"`
	Deleted           bool   `json:"deleted"`
	FileCount         int    `json:"fileCount"`
	SubDirectoryCount int    `json:"subDirectoryCount"`
	TotalSize         int64  `json:"totalSize"`
}

// ArchiveFile is one entry of GET /v1/api/folder/{id}/filelist. Files and
// folders share a single id space; folders have Soort "D" and no size.
type ArchiveFile struct {
	KoppelCount int    `json:"koppelCount"`
	ID          int    `json:"id"`
	ParentID    int    `json:"parentId"`
	Soort       string `json:"soort"`
	Naam        string `json:"naam"`
	Datum       string `json:"datum"`
	Size        int64  `json:"size"`
	IsDeleted   bool   `json:"isDeleted"`
}

// GetArchiveFolders fetches all digital archive folders.
func (c *Client) GetArchiveFolders() (json.RawMessage, error) {
	return c.apiGet("/v1/api/folder/directory")
}

// CreateArchiveFolder creates a new folder in the digital archive.
func (c *Client) CreateArchiveFolder(payload json.RawMessage) (json.RawMessage, error) {
	return c.apiPost("/v1/api/folder/directory", payload)
}

// GetArchiveFiles lists files in an archive folder.
func (c *Client) GetArchiveFiles(folderID int) (json.RawMessage, error) {
	return c.apiGet(fmt.Sprintf("/v1/api/folder/%d/filelist?includeDelete=false", folderID))
}

// UploadArchiveFile uploads a file (JSON with base64) to the digital archive.
func (c *Client) UploadArchiveFile(payload json.RawMessage) (json.RawMessage, error) {
	return c.apiPost("/v1/api/folder/upload", payload)
}

// GetFolderHeader fetches the header of an archive folder, including the
// administration's "Mail & herken" folder id. Any existing folder id works,
// including 0 for the root.
func (c *Client) GetFolderHeader(folderID int) (*FolderHeader, error) {
	raw, err := c.apiGet(fmt.Sprintf("/v1/api/folder/%d/header", folderID))
	if err != nil {
		return nil, err
	}
	var header FolderHeader
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, fmt.Errorf("parsing folder header: %w", err)
	}
	return &header, nil
}

// SetMailFolder makes folderID the target folder for "Mail & herken". The
// endpoint takes the bare folder id as its JSON body.
func (c *Client) SetMailFolder(folderID int) error {
	raw, err := c.apiPost("/v1/api/administratie/menhfolder", json.RawMessage(strconv.Itoa(folderID)))
	if err != nil {
		return err
	}
	return expectTrue(raw, "setting mail folder")
}

// ListArchiveFiles returns the files in an archive folder, leaving out
// subfolders and deleted entries. The result is never nil.
func (c *Client) ListArchiveFiles(folderID int) ([]ArchiveFile, error) {
	raw, err := c.GetArchiveFiles(folderID)
	if err != nil {
		return nil, err
	}
	var entries []ArchiveFile
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parsing filelist: %w", err)
	}
	files := make([]ArchiveFile, 0, len(entries))
	for _, e := range entries {
		if e.Soort == "F" && !e.IsDeleted {
			files = append(files, e)
		}
	}
	return files, nil
}

// DownloadArchiveFile fetches the raw bytes of an archive file, refusing files
// larger than maxBytes. See apiGetBinary for the return values.
func (c *Client) DownloadArchiveFile(fileID int, maxBytes int64) (data []byte, contentType, filename string, err error) {
	return c.apiGetBinary(fmt.Sprintf("/v1/api/folder/%d/download", fileID), maxBytes)
}

// RenameArchiveFile renames a file in place. The endpoint calls the file id
// "folderId" because files and folders share one id space.
func (c *Client) RenameArchiveFile(fileID, parentFolderID int, name string) error {
	payload, err := json.Marshal(struct {
		FolderID       int    `json:"folderId"`
		ParentFolderID int    `json:"parentFolderId"`
		Name           string `json:"name"`
	}{fileID, parentFolderID, name})
	if err != nil {
		return fmt.Errorf("encoding rename payload: %w", err)
	}
	raw, err := c.apiPut("/v1/api/folder/filename", payload)
	if err != nil {
		return err
	}
	return expectTrue(raw, "rename")
}

// bulkUpdateResult is the response of POST /v1/api/folder/bulk/update/{id}.
// The message arrays are kept raw because only their emptiness has been
// observed; a non-string element must not break the parse.
type bulkUpdateResult struct {
	UpdatedIDs    []int             `json:"updatedIds"`
	NotFoundIDs   []int             `json:"notFoundIds"`
	ErrorMessages []json.RawMessage `json:"errorMessages"`
}

// MoveArchiveFiles moves files into targetFolderID in one call. Files keep
// their id, so links to mutations stay intact. It returns the ids e-boekhouden
// reports as updated. An error is returned when e-boekhouden reports an error
// message, when an id was not found, or when a requested id is missing from
// the updated list; moved then still lists what did succeed.
func (c *Client) MoveArchiveFiles(targetFolderID int, fileIDs []int) (moved []int, err error) {
	if len(fileIDs) == 0 {
		return []int{}, nil
	}
	payload, err := json.Marshal(fileIDs)
	if err != nil {
		return nil, fmt.Errorf("encoding move payload: %w", err)
	}
	raw, err := c.apiPost(fmt.Sprintf("/v1/api/folder/bulk/update/%d", targetFolderID), payload)
	if err != nil {
		return nil, err
	}
	var res bulkUpdateResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("parsing bulk update response: %w", err)
	}
	moved = res.UpdatedIDs
	if moved == nil {
		moved = []int{}
	}
	if len(res.ErrorMessages) > 0 {
		// Typed so handlers can log it without the message, which may echo
		// a file name.
		return moved, &APIError{Path: "/v1/api/folder/bulk/update", Status: 200, Message: capMessage(joinRawMessages(res.ErrorMessages)), BodyLen: len(raw)}
	}
	if len(res.NotFoundIDs) > 0 {
		return moved, fmt.Errorf("e-Boekhouden: files not found: %v", res.NotFoundIDs)
	}
	updated := make(map[int]bool, len(moved))
	for _, id := range moved {
		updated[id] = true
	}
	for _, id := range fileIDs {
		if !updated[id] {
			return moved, fmt.Errorf("e-Boekhouden: file %d was not moved", id)
		}
	}
	return moved, nil
}

// DeleteArchiveFile permanently deletes a file from the digital archive.
func (c *Client) DeleteArchiveFile(fileID int) error {
	raw, err := c.apiDelete(fmt.Sprintf("/v1/api/folder/%d", fileID))
	if err != nil {
		return err
	}
	return expectTrue(raw, "delete")
}

// expectTrue checks the bare boolean body that the folder endpoints return.
// A literal false is a silent failure upstream, so it is surfaced as an error.
func expectTrue(raw json.RawMessage, action string) error {
	var ok bool
	if err := json.Unmarshal(raw, &ok); err != nil {
		return fmt.Errorf("e-Boekhouden: unexpected response to %s", action)
	}
	if !ok {
		return fmt.Errorf("e-Boekhouden: %s was not accepted", action)
	}
	return nil
}

// joinRawMessages renders upstream message elements as one string. String
// elements are unquoted; anything else is kept as its JSON text.
func joinRawMessages(msgs []json.RawMessage) string {
	parts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		var s string
		if err := json.Unmarshal(m, &s); err != nil {
			s = string(m)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "; ")
}
