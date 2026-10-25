package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

// ArchiveHandler holds the dependencies needed to clear a stale e-boekhouden
// session when a digital-archive endpoint is hit, so the frontend gets a
// reconnect prompt instead of a bare 502. It follows BankStatementsHandler.
type ArchiveHandler struct {
	sessions *session.Store
	db       *database.DB
}

// NewArchiveHandler creates a new digital archive handler.
func NewArchiveHandler(sessions *session.Store, db *database.DB) *ArchiveHandler {
	return &ArchiveHandler{sessions: sessions, db: db}
}

func (h *ArchiveHandler) handleEBError(c *gin.Context, err error) bool {
	return HandleEBoekhoudenSessionExpired(c, session.FromContext(c), h.sessions, h.db, err)
}

// GetArchiveFolders handles GET /api/v1/archive/folders
func (h *ArchiveHandler) GetArchiveFolders(c *gin.Context) {
	client := session.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "eboekhouden_not_connected"})
		return
	}

	raw, err := client.GetArchiveFolders()
	if err != nil {
		if h.handleEBError(c, err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}

// CreateArchiveFolder handles POST /api/v1/archive/folders
func (h *ArchiveHandler) CreateArchiveFolder(c *gin.Context) {
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
		if h.handleEBError(c, err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}

// GetArchiveFiles handles GET /api/v1/archive/files/:folderId
func (h *ArchiveHandler) GetArchiveFiles(c *gin.Context) {
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
		if h.handleEBError(c, err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}

// UploadArchiveFile handles POST /api/v1/archive/upload
func (h *ArchiveHandler) UploadArchiveFile(c *gin.Context) {
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
		if h.handleEBError(c, err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}

// LinkFileToMutation handles POST /api/v1/archive/link
func (h *ArchiveHandler) LinkFileToMutation(c *gin.Context) {
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
		if h.handleEBError(c, err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", raw)
}
