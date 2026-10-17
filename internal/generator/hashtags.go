package generator

import (
	"sort"
	"strings"

	"github.com/clodura/linkedin-post-agent/internal/models"
)

// LinkedIn rewards a few relevant hashtags over a wall of them: 3-5 targeted
// tags put a post in front of people who follow those subjects, while 10+
// reads as spam. For reach we use the usual mix: up to 3 tags specific to the
// topic, one broad, widely-followed tag for the field (e.g.
// #ArtificialIntelligence), and one very high-follower tag (e.g. #Technology).
const (
	minHashtags     = 4
	maxHashtags     = 5
	maxSpecificTags = 3
)

// hashtagCatalog maps topic words/phrases to popular, widely-followed LinkedIn
// hashtags. Keys are matched as whole words, so "ai" won't match "maintain".
// Ambiguous everyday words ("react", "node", "go") are deliberately left out.
var hashtagCatalog = []struct {
	keys []string
	tag  string
}{
	{[]string{"ai", "artificial intelligence"}, "#ArtificialIntelligence"},
	{[]string{"generative ai", "genai", "gen ai", "llm", "llms", "large language model", "large language models"}, "#GenerativeAI"},
	{[]string{"llm", "llms", "large language model", "large language models", "chatgpt", "gpt"}, "#LLM"},
	{[]string{"ai agent", "ai agents", "agentic", "multi-agent"}, "#AIAgents"},
	{[]string{"machine learning", "ml", "deep learning"}, "#MachineLearning"},
	{[]string{"prompt engineering", "prompting"}, "#PromptEngineering"},
	{[]string{"kubernetes", "k8s", "helm"}, "#Kubernetes"},
	{[]string{"docker", "container", "containers", "containerization"}, "#Docker"},
	{[]string{"devops"}, "#DevOps"},
	{[]string{"ci/cd", "cicd", "continuous integration", "continuous delivery", "continuous deployment", "github actions"}, "#CICD"},
	{[]string{"cloud", "cloud computing", "cloud native", "multi-cloud"}, "#CloudComputing"},
	{[]string{"aws", "amazon web services"}, "#AWS"},
	{[]string{"azure"}, "#Azure"},
	{[]string{"gcp", "google cloud"}, "#GoogleCloud"},
	{[]string{"serverless", "faas"}, "#Serverless"},
	{[]string{"terraform", "infrastructure as code", "iac", "pulumi"}, "#InfrastructureAsCode"},
	{[]string{"platform engineering", "internal developer platform"}, "#PlatformEngineering"},
	{[]string{"sre", "site reliability", "on-call", "oncall", "incident", "incidents", "outage", "outages", "postmortem"}, "#SRE"},
	{[]string{"observability", "monitoring", "tracing", "opentelemetry", "logging"}, "#Observability"},
	{[]string{"microservice", "microservices", "monolith", "monoliths"}, "#Microservices"},
	{[]string{"system design", "scalability", "distributed system", "distributed systems", "load balancer", "sharding"}, "#SystemDesign"},
	{[]string{"software architecture", "architecture", "event-driven", "domain-driven design", "ddd"}, "#SoftwareArchitecture"},
	{[]string{"api", "apis", "rest api", "rest apis", "grpc", "webhooks"}, "#APIs"},
	{[]string{"graphql"}, "#GraphQL"},
	{[]string{"database", "databases", "indexing", "sql query", "sql queries"}, "#Databases"},
	{[]string{"sql", "mysql"}, "#SQL"},
	{[]string{"postgres", "postgresql"}, "#PostgreSQL"},
	{[]string{"mongodb"}, "#MongoDB"},
	{[]string{"redis"}, "#Redis"},
	{[]string{"kafka", "event streaming"}, "#ApacheKafka"},
	{[]string{"data engineering", "data pipeline", "data pipelines", "etl", "data warehouse"}, "#DataEngineering"},
	{[]string{"security", "cybersecurity", "vulnerability", "vulnerabilities", "zero trust", "breach", "ransomware"}, "#CyberSecurity"},
	{[]string{"appsec", "application security", "owasp", "authentication", "oauth", "secrets"}, "#AppSec"},
	{[]string{"javascript", "js", "ecmascript"}, "#JavaScript"},
	{[]string{"typescript"}, "#TypeScript"},
	{[]string{"reactjs", "react.js", "react hooks", "react native", "react component", "react components"}, "#ReactJS"},
	{[]string{"nextjs", "next.js"}, "#NextJS"},
	{[]string{"node.js", "nodejs"}, "#NodeJS"},
	{[]string{"python", "django", "fastapi", "flask"}, "#Python"},
	{[]string{"golang", "go language", "goroutine", "goroutines"}, "#Golang"},
	{[]string{"rust", "rustlang"}, "#RustLang"},
	{[]string{"java", "spring boot", "jvm"}, "#Java"},
	{[]string{"kotlin"}, "#Kotlin"},
	{[]string{"ios", "swiftui", "xcode"}, "#iOSDevelopment"},
	{[]string{"android", "jetpack compose"}, "#AndroidDev"},
	{[]string{"frontend", "front-end", "css", "html"}, "#FrontendDevelopment"},
	{[]string{"backend", "back-end"}, "#BackendDevelopment"},
	{[]string{"web development", "web dev", "webdev", "web app", "web apps"}, "#WebDevelopment"},
	{[]string{"mobile development", "mobile app", "mobile apps"}, "#MobileDevelopment"},
	{[]string{"testing", "unit test", "unit tests", "integration test", "integration tests", "test coverage", "qa", "flaky"}, "#SoftwareTesting"},
	{[]string{"tdd", "test-driven", "test driven"}, "#TDD"},
	{[]string{"clean code", "readability", "code quality", "naming"}, "#CleanCode"},
	{[]string{"refactoring", "refactor", "technical debt", "tech debt", "legacy code", "legacy"}, "#TechnicalDebt"},
	{[]string{"code review", "code reviews", "pull request", "pull requests", "pr", "prs"}, "#CodeReview"},
	{[]string{"git", "github", "gitlab", "version control", "merge conflict"}, "#Git"},
	{[]string{"open source", "open-source", "oss", "maintainer", "maintainers"}, "#OpenSource"},
	{[]string{"linux", "bash", "shell"}, "#Linux"},
	{[]string{"algorithm", "algorithms", "dsa", "data structures", "leetcode", "big o"}, "#DSA"},
	{[]string{"interview", "interviews", "interviewing", "hiring", "recruiter", "recruiters", "resume"}, "#TechInterviews"},
	{[]string{"career", "careers", "promotion", "promoted", "career growth", "senior engineer", "staff engineer"}, "#CareerGrowth"},
	{[]string{"leadership", "tech lead", "engineering manager", "managers", "mentoring", "mentor", "mentorship"}, "#Leadership"},
	{[]string{"engineering culture", "team culture", "culture"}, "#EngineeringCulture"},
	{[]string{"productivity", "deep work", "focus time", "meetings"}, "#Productivity"},
	{[]string{"remote work", "remote-first", "hybrid work", "work from home", "wfh"}, "#RemoteWork"},
	{[]string{"startup", "startups", "founder", "founders", "mvp"}, "#Startups"},
	{[]string{"developer experience", "devex", "developer tools", "dev tools", "tooling", "ide"}, "#DeveloperExperience"},
	{[]string{"product management", "product manager", "product managers"}, "#ProductManagement"},
	{[]string{"ux", "user experience", "design system", "design systems"}, "#UXDesign"},
	{[]string{"accessibility", "a11y"}, "#Accessibility"},
	{[]string{"burnout", "mental health"}, "#MentalHealth"},
	{[]string{"learn to code", "beginner", "beginners", "bootcamp", "junior developer", "junior developers", "self-taught"}, "#LearnToCode"},
	{[]string{"web performance", "latency", "caching", "cache", "p99", "throughput"}, "#PerformanceEngineering"},
	{[]string{"blockchain", "web3", "smart contract", "smart contracts"}, "#Blockchain"},
	{[]string{"openai"}, "#OpenAI"},
	{[]string{"game", "games", "gaming", "video game", "video games", "esports", "starcraft"}, "#Gaming"},
}

// fieldTags are broad, widely-followed tags for a whole field. A post gets
// one of them so it also reaches that wider audience.
var fieldTags = map[string]bool{
	"artificialintelligence": true, "cybersecurity": true, "cloudcomputing": true,
	"webdevelopment": true, "softwareengineering": true, "softwaredevelopment": true,
	"programming": true, "datascience": true, "leadership": true, "careergrowth": true,
}

// reachTags have the largest audiences on LinkedIn. One rounds off each post.
var reachTags = map[string]bool{"technology": true, "tech": true, "innovation": true}

// fillerTags are broad but crowded tags we never spend a slot on.
var fillerTags = map[string]bool{
	"coding": true, "developers": true, "developer": true, "developercommunity": true,
	"techcommunity": true, "computerscience": true, "engineering": true,
}

// fieldOf maps a specific tag (lowercase, no #) to the field tag that best
// widens its reach; anything unlisted belongs to #SoftwareEngineering.
var fieldOf = map[string]string{
	"llm": "#ArtificialIntelligence", "generativeai": "#ArtificialIntelligence",
	"aiagents": "#ArtificialIntelligence", "machinelearning": "#ArtificialIntelligence",
	"promptengineering": "#ArtificialIntelligence",
	"appsec":            "#CyberSecurity",
	"kubernetes":        "#CloudComputing", "docker": "#CloudComputing", "devops": "#CloudComputing",
	"cicd": "#CloudComputing", "aws": "#CloudComputing", "azure": "#CloudComputing",
	"googlecloud": "#CloudComputing", "serverless": "#CloudComputing",
	"infrastructureascode": "#CloudComputing", "platformengineering": "#CloudComputing",
	"sre": "#CloudComputing", "observability": "#CloudComputing",
	"reactjs": "#WebDevelopment", "nextjs": "#WebDevelopment", "javascript": "#WebDevelopment",
	"typescript": "#WebDevelopment", "frontenddevelopment": "#WebDevelopment",
	"nodejs": "#WebDevelopment", "uxdesign": "#WebDevelopment", "accessibility": "#WebDevelopment",
	"dataengineering": "#DataScience",
	"techinterviews":  "#CareerGrowth", "learntocode": "#CareerGrowth", "mentalhealth": "#CareerGrowth",
	"remotework": "#CareerGrowth", "productivity": "#CareerGrowth",
	"engineeringculture": "#Leadership", "productmanagement": "#Leadership", "startups": "#Leadership",
}

// reachFor picks the high-follower tag for a field: career and leadership
// posts reach engineers best through #SoftwareEngineering, the rest through
// #Technology.
func reachFor(field string) string {
	if field == "#CareerGrowth" || field == "#Leadership" {
		return "#SoftwareEngineering"
	}
	return "#Technology"
}

// tagAliases fold common variants into one canonical, widely-followed tag so a
// post never spends two slots on the same audience (#LLM and #LLMs).
var tagAliases = map[string]string{
	"ai": "#ArtificialIntelligence", "artificialintelligence": "#ArtificialIntelligence",
	"llms": "#LLM", "largelanguagemodel": "#LLM", "largelanguagemodels": "#LLM",
	"genai": "#GenerativeAI", "aiagent": "#AIAgents", "agenticai": "#AIAgents", "agents": "#AIAgents",
	"ml": "#MachineLearning", "k8s": "#Kubernetes", "js": "#JavaScript", "ts": "#TypeScript",
	"react": "#ReactJS", "node": "#NodeJS", "go": "#Golang", "rust": "#RustLang",
	"security": "#CyberSecurity", "infosec": "#CyberSecurity", "swe": "#SoftwareEngineering",
	"softwaredev": "#SoftwareDevelopment", "tech": "#Technology", "systemsdesign": "#SystemDesign",
	"cloud": "#CloudComputing", "api": "#APIs", "database": "#Databases", "testing": "#SoftwareTesting",
	"career": "#CareerGrowth",
}

// junkTags are words that sneak in as hashtags from headlines but carry no
// audience (e.g. "#Show #HN" from "Show HN: ...").
var junkTags = map[string]bool{
	"the": true, "and": true, "for": true, "how": true, "why": true, "what": true,
	"new": true, "show": true, "hn": true, "ask": true, "news": true, "post": true,
	"linkedin": true, "thread": true, "this": true, "that": true, "your": true,
	"you": true, "with": true, "from": true, "about": true, "into": true, "update": true,
}

// catalogByTag lets a model-suggested tag pick up the catalog's canonical casing.
var catalogByTag = func() map[string]string {
	m := make(map[string]string, len(hashtagCatalog))
	for _, e := range hashtagCatalog {
		m[strings.ToLower(strings.TrimPrefix(e.tag, "#"))] = e.tag
	}
	for _, t := range tagAliases {
		m[strings.ToLower(strings.TrimPrefix(t, "#"))] = t
	}
	return m
}()

// normalizeForMatch lowercases text and reduces it to space-separated tokens,
// keeping in-word punctuation that matters for tech names (node.js, ci/cd).
func normalizeForMatch(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '/', r == '-', r == '+':
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	fields := strings.Fields(b.String())
	for i, f := range fields {
		fields[i] = strings.Trim(f, ".-/")
	}
	return " " + strings.Join(fields, " ") + " "
}

// cleanHashtag reduces a model-suggested tag to letters/digits (LinkedIn breaks
// hashtags at punctuation, e.g. "#CI/CD") and rejects junk. Returns "" if unusable.
func cleanHashtag(tag string) string {
	var b strings.Builder
	for _, r := range strings.TrimPrefix(strings.TrimSpace(tag), "#") {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	t := b.String()
	lower := strings.ToLower(t)
	if len(t) < 2 || len(t) > 30 || junkTags[lower] || strings.Trim(t, "0123456789") == "" {
		return ""
	}
	if canon, ok := tagAliases[lower]; ok {
		return canon
	}
	if canon, ok := catalogByTag[lower]; ok {
		return canon
	}
	return "#" + t
}

// foldKey identifies tags that share an audience: case-insensitive and
// ignoring a plural "s" (#APIs/#API, #Microservices/#Microservice).
func foldKey(tag string) string {
	k := strings.ToLower(strings.TrimPrefix(tag, "#"))
	if len(k) > 4 && strings.HasSuffix(k, "s") && !strings.HasSuffix(k, "ss") {
		k = k[:len(k)-1]
	}
	return k
}

func tagKind(tag string) (field, reach, filler bool) {
	k := strings.ToLower(strings.TrimPrefix(tag, "#"))
	return fieldTags[k], reachTags[k], fillerTags[k]
}

// selectHashtags picks 4-5 hashtags for reach: up to 3 that best match the
// topic (catalog tags found in the title/summary/post, title weighted highest,
// merged with the model's own picks), one broad tag for the field, and one
// very high-follower tag. Variants of the same tag are folded together.
func selectHashtags(modelTags []string, topic models.Topic, body string) []string {
	type cand struct {
		tag   string
		score float64
		order int
	}
	cands := map[string]*cand{}
	add := func(tag string, score float64) {
		key := foldKey(tag)
		if c, ok := cands[key]; ok {
			c.score += score
			return
		}
		cands[key] = &cand{tag: tag, score: score, order: len(cands)}
	}

	title := normalizeForMatch(topic.Title)
	summary := normalizeForMatch(topic.Summary)
	keywords := normalizeForMatch(strings.Join(topic.Keywords, " , "))
	post := normalizeForMatch(body)
	for _, e := range hashtagCatalog {
		score := 0.0
		for _, k := range e.keys {
			needle := " " + k + " "
			if strings.Contains(title, needle) {
				score += 6
			}
			if strings.Contains(summary, needle) {
				score += 3
			}
			if strings.Contains(keywords, needle) {
				score += 1
			}
			if n := strings.Count(post, needle); n > 0 {
				score += float64(min(n, 3))
			}
		}
		if score > 0 {
			add(e.tag, score)
		}
	}
	for i, t := range modelTags {
		if t = cleanHashtag(t); t != "" {
			add(t, 4-float64(i)*0.2) // the model's first picks are its best
		}
	}

	all := make([]*cand, 0, len(cands))
	for _, c := range cands {
		all = append(all, c)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].order < all[j].order
	})

	var specific, fields []string
	for _, c := range all {
		field, reach, filler := tagKind(c.tag)
		switch {
		case field:
			fields = append(fields, c.tag)
		case reach || filler:
		case len(specific) < maxSpecificTags:
			specific = append(specific, c.tag)
		}
	}

	// The field tag: the best-matching one, else the field of the top topic tag.
	field := "#SoftwareEngineering"
	if len(fields) > 0 {
		field = fields[0]
	} else if len(specific) > 0 {
		if f, ok := fieldOf[strings.ToLower(strings.TrimPrefix(specific[0], "#"))]; ok {
			field = f
		}
	}

	out := append([]string{}, specific...)
	have := map[string]bool{}
	for _, t := range out {
		have[foldKey(t)] = true
	}
	push := func(t string) {
		if len(out) < maxHashtags && !have[foldKey(t)] {
			have[foldKey(t)] = true
			out = append(out, t)
		}
	}
	push(field)
	push(reachFor(field))
	for _, t := range append(fields, "#SoftwareEngineering", "#Programming", "#SoftwareDevelopment") {
		if len(out) >= minHashtags {
			break
		}
		push(t)
	}
	return out
}
