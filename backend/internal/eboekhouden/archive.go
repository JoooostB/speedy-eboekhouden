package eboekhouden

import (
	"encoding/json"
	"fmt"
)

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
