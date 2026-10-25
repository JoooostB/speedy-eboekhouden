package handler

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
	"github.com/joooostb/speedy-eboekhouden/internal/storage"
)

// avatarStore is the part of the R2 client the avatar handler needs.
// *storage.Client satisfies it; tests supply a fake.
type avatarStore interface {
	Upload(ctx context.Context, key string, data []byte, contentType string) error
	Delete(ctx context.Context, key string) error
	PublicObjectURL(key string) string
}

// avatarUsers is the part of the database the avatar handler needs.
type avatarUsers interface {
	GetUserByID(ctx context.Context, id string) (*database.User, error)
	SetAvatarKey(ctx context.Context, userID, key string) error
}

// AvatarHandler handles avatar upload/retrieval via R2.
type AvatarHandler struct {
	// r2 is nil when R2 is not configured.
	r2 avatarStore
	db avatarUsers
}

// NewAvatarHandler creates a new avatar handler. r2 may be nil.
func NewAvatarHandler(r2 *storage.Client, db *database.DB) *AvatarHandler {
	h := &AvatarHandler{db: db}
	// Only a non-nil client goes into the interface: a typed nil would make
	// h.r2 != nil and skip the "no storage" answer.
	if r2 != nil {
		h.r2 = r2
	}
	return h
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
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Deze server heeft geen bestandsopslag voor foto's."})
		return
	}

	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	file, header, err := c.Request.FormFile("avatar")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Kies een foto."})
		return
	}
	defer file.Close()

	// Validate content type
	contentType := header.Header.Get("Content-Type")
	if !allowedImageTypes[contentType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Kies een JPG-, PNG- of WebP-bestand."})
		return
	}

	// Read with size limit
	data, err := io.ReadAll(io.LimitReader(file, maxAvatarSize+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Het bestand kon niet worden gelezen. Probeer het opnieuw."})
		return
	}
	if len(data) > maxAvatarSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "De foto is te groot. Kies een bestand tot 2 MB."})
		return
	}

	// Generate unique key — always derive extension from validated content-type, not filename
	ext := extensionForType(contentType)
	key := fmt.Sprintf("avatars/%s%s", uuid.New().String(), ext)

	ctx := c.Request.Context()

	// Upload to R2
	if err := h.r2.Upload(ctx, key, data, contentType); err != nil {
		log.Printf("Avatar: upload of %s for user %s failed: %v", key, sess.UserID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Uploaden is mislukt. Probeer het opnieuw."})
		return
	}

	// Point the profile at the new object first and only then remove the
	// old one: deleting first left the profile pointing at a deleted object
	// whenever the database update failed. A failed update removes the new
	// object again, so the profile keeps its old, still existing photo.
	var oldKey string
	if oldUser, err := h.db.GetUserByID(ctx, sess.UserID); err == nil && oldUser != nil {
		oldKey = oldUser.AvatarKey
	}
	if err := h.db.SetAvatarKey(ctx, sess.UserID, key); err != nil {
		log.Printf("Avatar: saving %s for user %s failed: %v", key, sess.UserID, err)
		h.deleteAvatarObject(ctx, key)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "De foto kon niet worden opgeslagen. Probeer het opnieuw."})
		return
	}
	if oldKey != "" && oldKey != key {
		h.deleteAvatarObject(ctx, oldKey)
	}

	c.JSON(http.StatusOK, gin.H{
		"avatarKey": key,
		"avatarUrl": h.r2.PublicObjectURL(key),
	})
}

// deleteAvatarObject removes an avatar object from R2, best-effort: an
// object left behind costs storage, not correctness. Keys are
// avatars/{uuid}.{ext} and carry no personal data, so they are logged.
func (h *AvatarHandler) deleteAvatarObject(ctx context.Context, key string) {
	if err := h.r2.Delete(ctx, key); err != nil {
		log.Printf("Avatar: deleting %s failed: %v", key, err)
	}
}

// Delete handles DELETE /api/v1/avatar — removes avatar.
func (h *AvatarHandler) Delete(c *gin.Context) {
	if h.r2 == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Deze server heeft geen bestandsopslag voor foto's."})
		return
	}

	sess := session.FromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	ctx := c.Request.Context()
	user, err := h.db.GetUserByID(ctx, sess.UserID)
	if err != nil || user.AvatarKey == "" {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}

	// Clear the profile first, as in Upload: if that fails the photo stays
	// complete and the user can try again; the object goes only once nothing
	// points at it.
	if err := h.db.SetAvatarKey(ctx, sess.UserID, ""); err != nil {
		log.Printf("Avatar: clearing the key of user %s failed: %v", sess.UserID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "De foto kon niet worden verwijderd. Probeer het opnieuw."})
		return
	}
	h.deleteAvatarObject(ctx, user.AvatarKey)

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
