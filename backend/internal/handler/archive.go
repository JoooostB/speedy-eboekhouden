package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// GetArchiveFolders handles GET /api/v1/archive/folders
func GetArchiveFolders(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	raw, err := client.GetArchiveFolders()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}

// CreateArchiveFolder handles POST /api/v1/archive/folders
func CreateArchiveFolder(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	raw, err := client.CreateArchiveFolder(json.RawMessage(body))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}

// GetArchiveFiles handles GET /api/v1/archive/files/:folderId
func GetArchiveFiles(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	folderID, err := strconv.Atoi(c.Param("folderId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid folder ID"})
		return
	}

	raw, err := client.GetArchiveFiles(folderID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}

// UploadArchiveFile handles POST /api/v1/archive/upload
func UploadArchiveFile(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	raw, err := client.UploadArchiveFile(json.RawMessage(body))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}

// LinkFileToMutation handles POST /api/v1/archive/link
func LinkFileToMutation(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	raw, err := client.LinkFileToMutation(json.RawMessage(body))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}
