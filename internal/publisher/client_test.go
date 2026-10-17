package publisher

import (
	"strings"
	"testing"

	"github.com/clodura/linkedin-post-agent/internal/models"
)

func TestComposeBodyTitle(t *testing.T) {
	d := &models.Draft{Title: "Ship small", Body: "Tiny PRs merge faster.", Hashtags: []string{"#CodeReview", "#SoftwareEngineering"}}

	// Existing drafts (HideTitle unset) keep the bold title line.
	got := composeBody(d)
	if !strings.HasPrefix(got, toBoldUnicode("Ship small")+"\n\n") {
		t.Errorf("title missing for default draft:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n\n#CodeReview #SoftwareEngineering") {
		t.Errorf("hashtags not appended:\n%s", got)
	}

	d.HideTitle = true
	got = composeBody(d)
	if !strings.HasPrefix(got, "Tiny PRs merge faster.") {
		t.Errorf("HideTitle should start directly with the body:\n%s", got)
	}
}
