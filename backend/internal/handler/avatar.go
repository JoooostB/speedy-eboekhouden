package handler

import (
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
	"github.com/joooostb/speedy-eboekhouden/internal/storage"
)

// AvatarHandler handles avatar upload/retrieval via R2.
type AvatarHandler struct {
	r2 *storage.Client
	db *database.DB
}

// NewAvatarHandler creates a new avatar handler.
func NewAvatarHandler(r2 *storage.Client, db *database.DB) *AvatarHandler {
	return &AvatarHandler{r2: r2, db: db}
}

const maxAvatarSize = 2 << 20 // 2 MB

var allowedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

// Upload handles POST /api/v1/avatar — uploads avatar to R2.
func (h *AvatarHandler) Upload(c *gin.Context) {
	if h.r2 == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Bestandsopslag is niet geconfigureerd"})
		return
	}

	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	file, header, err := c.Request.FormFile("avatar")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Afbeelding is verplicht"})
		return
	}
	defer file.Close()

	// Validate content type
	contentType := header.Header.Get("Content-Type")
	if !allowedImageTypes[contentType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Alleen JPEG, PNG en WebP zijn toegestaan"})
		return
	}

	// Read with size limit
	data, err := io.ReadAll(io.LimitReader(file, maxAvatarSize+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Bestand kon niet worden gelezen"})
		return
	}
	if len(data) > maxAvatarSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Afbeelding is te groot (max 2 MB)"})
		return
	}

	// Generate unique key — always derive extension from validated content-type, not filename
	ext := extensionForType(contentType)
	key := fmt.Sprintf("avatars/%s%s", uuid.New().String(), ext)

	// Upload to R2
	if err := h.r2.Upload(c.Request.Context(), key, data, contentType); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Upload mislukt"})
		return
	}

	// Delete old avatar if exists
	oldUser, _ := h.db.GetUserByID(c.Request.Context(), sess.UserID)
	if oldUser != nil && oldUser.AvatarKey != "" {
		h.r2.Delete(c.Request.Context(), oldUser.AvatarKey)
	}

	// Save key to database
	if err := h.db.SetAvatarKey(c.Request.Context(), sess.UserID, key); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Avatar opslaan mislukt"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"avatarKey": key,
		"avatarUrl": h.r2.PublicObjectURL(key),
	})
}

// Delete handles DELETE /api/v1/avatar — removes avatar.
func (h *AvatarHandler) Delete(c *gin.Context) {
	if h.r2 == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Bestandsopslag is niet geconfigureerd"})
		return
	}

	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	user, err := h.db.GetUserByID(c.Request.Context(), sess.UserID)
	if err != nil || user.AvatarKey == "" {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}

	h.r2.Delete(c.Request.Context(), user.AvatarKey)
	h.db.SetAvatarKey(c.Request.Context(), sess.UserID, "")

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func extensionForType(ct string) string {
	switch ct {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}
