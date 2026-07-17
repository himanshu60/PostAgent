package analyzer

type PostPattern struct {
	AvgLength       int
	AvgEmojiCount   int
	AvgHashtagCount int
	AvgLineBreaks   float64
	Tone            string
	StructureFormat string
	RecentBodies    []string
	PostCount       int
}
