package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"

	"github.com/clodura/linkedin-post-agent/internal/models"
)

type SelectResult struct {
	Draft        *models.Draft
	Action       string // publish | schedule | edit | reject | restart | skip
	ScheduledFor *time.Time
}

func SelectDraft(drafts []models.Draft) (*SelectResult, error) {
	if len(drafts) == 0 {
		return &SelectResult{Action: "skip"}, nil
	}

	options := make([]string, 0, len(drafts)+3)
	for i, d := range drafts {
		preview := d.Body
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		options = append(options, fmt.Sprintf("[%d] %s", i+1, preview))
	}
	options = append(options, "───────────")
	options = append(options, "[6] Skip this cycle")
	options = append(options, "[7] Regenerate topics")

	prompt := &survey.Select{
		Message: "Select a post to publish:",
		Options: options,
	}

	var choice int
	if err := survey.AskOne(prompt, &choice, survey.WithPageSize(10)); err != nil {
		if err == terminal.InterruptErr {
			return &SelectResult{Action: "skip"}, nil
		}
		return nil, err
	}

	if choice >= len(drafts) {
		skipIdx := len(drafts)
		regenIdx := len(drafts) + 1
		switch {
		case choice == skipIdx:
			return &SelectResult{Action: "skip"}, nil
		case choice == regenIdx:
			return &SelectResult{Action: "restart"}, nil
		default:
			return &SelectResult{Action: "skip"}, nil
		}
	}

	selected := &drafts[choice]

	fmt.Println()
	fmt.Println("─── Preview ───")
	fmt.Println()
	fmt.Println(selected.Body)
	if len(selected.Hashtags) > 0 {
		fmt.Println()
		fmt.Println(strings.Join(selected.Hashtags, " "))
	}
	fmt.Println()

	mediaResult := promptMedia()
	selected.Media = mediaResult

	approvalResult := promptApproval(selected)

	return &SelectResult{
		Draft:        selected,
		Action:       approvalResult.Action,
		ScheduledFor: approvalResult.ScheduledFor,
	}, nil
}

func promptMedia() *models.Media {
	options := []string{
		"[1] Auto-fetch image from source article",
		"[2] Provide a different image URL",
		"[3] Provide a PDF URL",
		"[4] No media",
	}

	prompt := &survey.Select{
		Message: "Media:",
		Options: options,
	}

	var choice int
	if err := survey.AskOne(prompt, &choice); err != nil {
		return &models.Media{Type: models.MediaTypeNone}
	}

	switch choice {
	case 0:
		return &models.Media{Type: models.MediaTypeImage, SourceURL: "__auto__"}
	case 1:
		var url string
		survey.AskOne(&survey.Input{Message: "Image URL:"}, &url, survey.WithValidator(survey.Required))
		return &models.Media{Type: models.MediaTypeImage, SourceURL: url}
	case 2:
		var url string
		survey.AskOne(&survey.Input{Message: "PDF URL:"}, &url, survey.WithValidator(survey.Required))
		return &models.Media{Type: models.MediaTypePDF, SourceURL: url}
	default:
		return &models.Media{Type: models.MediaTypeNone}
	}
}

type approvalResult struct {
	Action       string
	ScheduledFor *time.Time
}

func promptApproval(draft *models.Draft) approvalResult {
	fmt.Println("─── Approval ───")
	fmt.Println()

	options := []string{
		"[1] Approve & Publish now",
		"[2] Schedule for later",
		"[3] Edit post body",
		"[4] Reject",
		"[5] Start over (pick different topic)",
	}

	prompt := &survey.Select{
		Message: "Choose action:",
		Options: options,
	}

	var choice int
	if err := survey.AskOne(prompt, &choice); err != nil {
		return approvalResult{Action: "reject"}
	}

	switch choice {
	case 0:
		return approvalResult{Action: "publish"}
	case 1:
		return promptSchedule()
	case 2:
		return promptEdit(draft)
	case 3:
		return approvalResult{Action: "reject"}
	case 4:
		return approvalResult{Action: "restart"}
	default:
		return approvalResult{Action: "reject"}
	}
}

func promptSchedule() approvalResult {
	options := []string{
		"[1] Tomorrow at 9 AM",
		"[2] Next Monday at 9 AM",
		"[3] Next Wednesday at 9 AM",
		"[4] Next Friday at 9 AM",
		"[5] Custom date/time",
	}
	prompt := &survey.Select{
		Message: "Schedule for:",
		Options: options,
	}

	var choice int
	if err := survey.AskOne(prompt, &choice); err != nil {
		return approvalResult{Action: "reject"}
	}

	now := time.Now()
	var t time.Time

	switch choice {
	case 0:
		t = time.Date(now.Year(), now.Month(), now.Day()+1, 9, 0, 0, 0, now.Location())
	case 1:
		daysUntilMonday := (8 - int(now.Weekday())) % 7
		if daysUntilMonday == 0 {
			daysUntilMonday = 7
		}
		t = time.Date(now.Year(), now.Month(), now.Day()+daysUntilMonday, 9, 0, 0, 0, now.Location())
	case 2:
		daysUntilWednesday := (10 - int(now.Weekday())) % 7
		if daysUntilWednesday == 0 {
			daysUntilWednesday = 7
		}
		t = time.Date(now.Year(), now.Month(), now.Day()+daysUntilWednesday, 9, 0, 0, 0, now.Location())
	case 3:
		daysUntilFriday := (12 - int(now.Weekday())) % 7
		if daysUntilFriday == 0 {
			daysUntilFriday = 7
		}
		t = time.Date(now.Year(), now.Month(), now.Day()+daysUntilFriday, 9, 0, 0, 0, now.Location())
	case 4:
		var dateStr string
		survey.AskOne(&survey.Input{Message: "Date/time (YYYY-MM-DD HH:MM):"}, &dateStr, survey.WithValidator(survey.Required))
		parsed, err := time.Parse("2006-01-02 15:04", dateStr)
		if err != nil {
			fmt.Println("Invalid format, scheduling for tomorrow")
			t = time.Date(now.Year(), now.Month(), now.Day()+1, 9, 0, 0, 0, now.Location())
		} else {
			t = parsed
		}
	}

	return approvalResult{Action: "schedule", ScheduledFor: &t}
}

func promptEdit(draft *models.Draft) approvalResult {
	fmt.Println("Edit the post body (save and close to continue):")

	editorPrompt := &survey.Editor{
		Message:  "Edit post:",
		Default:  draft.Body,
		FileName: "post.md",
	}

	var edited string
	if err := survey.AskOne(editorPrompt, &edited); err != nil {
		return approvalResult{Action: "reject"}
	}

	draft.Body = edited
	fmt.Println("Post updated. Review again:")
	fmt.Println()
	fmt.Println(draft.Body)
	fmt.Println()

	return promptApproval(draft)
}
