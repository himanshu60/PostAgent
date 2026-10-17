package publisher

import (
	"context"
	"os"

	"github.com/clodura/linkedin-post-agent/internal/models"
	"github.com/clodura/linkedin-post-agent/internal/notecard"
)

// uploadNoteCard renders a handwritten-notes card to PNG and uploads it to
// LinkedIn, returning the image URN. Rendering is deterministic, so the
// published image matches the dashboard preview exactly.
func (c *Client) uploadNoteCard(ctx context.Context, card models.NoteCard) (string, error) {
	data, err := notecard.Render(card)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", "card_img_*.png")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	tmp.Close()
	return c.UploadImage(ctx, tmpPath)
}
