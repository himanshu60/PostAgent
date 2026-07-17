package research

import "github.com/clodura/linkedin-post-agent/internal/models"

type SourceType string

const (
	SourceRSS     SourceType = "rss"
	SourceNewsAPI SourceType = "newsapi"
	SourceScraped SourceType = "scraped"
)

type FetchedTopic struct {
	Title    string
	URL      string
	Summary  string
	Keywords []string
	ImageURL string
	Source   SourceType
}

func (f FetchedTopic) ToModel() models.Topic {
	return models.Topic{
		Title:    f.Title,
		URL:      f.URL,
		Summary:  f.Summary,
		Keywords: f.Keywords,
		ImageURL: f.ImageURL,
		Source:   string(f.Source),
	}
}
