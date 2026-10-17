# PostAgent — Project Guide

A deep dive into what PostAgent is, what it's built on, and how every part works
together. Read [README.md](README.md) first for the quick overview.

---

## 1. What the project is

PostAgent is an **autonomous LinkedIn posting agent** for a software-engineering
audience. It closes the full loop of content marketing with no human in the
critical path (unless you want one):

> **research trending topics → filter for relevance → write an on-brand post +
> image → (optionally) review → schedule → publish to LinkedIn.**

It runs as a single long-lived Go process that exposes a web dashboard and runs
a background scheduler. State lives in MongoDB, so the process is stateless
enough to restart safely and resume where it left off.

### What it's based on

| Layer | Technology | Why |
|---|---|---|
| Language / runtime | **Go 1.25** | Single static binary, great concurrency, trivial to containerize. |
| Datastore | **MongoDB** (`go.mongodb.org/mongo-driver`) | Flexible documents for drafts/topics/cycles; free Atlas tier. |
| Text LLM | **Gemini / OpenRouter / Ollama** (pluggable) | Provider-agnostic with automatic fallback on quota/outage. |
| Images | **Handwritten sketchnotes** (`internal/notecard`) / Gemini / OpenAI / Pollinations | Sketchnotes are rendered locally — free, legible, on-topic. |
| Publishing | **LinkedIn REST API** + OAuth2 | Official posting + native image/document upload. |
| Scheduling | **`robfig/cron/v3`** + poll loops | Cron for the daily job; short poll for due posts. |
| News/trends | **RSS (`gofeed`)** + **NewsAPI** | Developer-focused sources. |
| Logging | **`go.uber.org/zap`** | Structured production logs. |
| Config | **`godotenv`** + env vars | 12-factor style; `.env` locally, real env in prod. |

---

## 1a. Authentication & multi-tenancy

PostAgent is **multi-tenant**: each user is a tenant identified by their LinkedIn
account.

- **Sign in with LinkedIn (OpenID Connect).** One OAuth flow yields both the
  user's identity (`sub`, name, email, picture) *and* their posting tokens
  (`w_member_social`). The `sub` becomes the user's primary id. Handled in
  `internal/ui/web/auth.go`.
- **Sessions.** A random token is stored in `agent_sessions` and set as a secure,
  http-only cookie. `requireAuth` middleware loads the user for every protected
  route and injects it into the request context; anonymous visitors are sent to
  `/login`.
- **Data isolation.** Every draft/topic/published/cycle document carries a
  `user_id`. All list queries filter by it, and every by-id mutation verifies
  ownership — so one tenant can never read or change another's data.
- **Per-user publishing.** The scheduler resolves each due draft's owner and
  builds a LinkedIn client from *that user's* tokens (`PublisherFactory` in
  `cmd/agent`), so a post is always authored by its own account. Refreshed
  tokens are written back to the user record.
- **Generation is user-driven.** Users generate/schedule from the UI; there is
  no shared auto-generate cron.

## 2. High-level architecture

```
                         ┌──────────────────────────────────────────┐
                         │            cmd/agent (main)                │
                         │  flags: -serve | -once | -auth | (default) │
                         └───────────────┬────────────────────────────┘
                                         │ wires everything together
              ┌──────────────────────────┼───────────────────────────┐
              ▼                          ▼                           ▼
      ┌───────────────┐         ┌─────────────────┐        ┌──────────────────┐
      │  Web UI        │         │   Scheduler      │        │   Agent (core)    │
      │ internal/ui/web│◄───────►│ internal/        │───────►│ internal/agent    │
      │  :8081         │  shares │ scheduler        │ drives │  RunCycle()       │
      └───────────────┘  agent  └─────────────────┘        │  EnsureDailyPost()│
                                                            │  PublishScheduled()│
                                                            └───────┬───────────┘
             ┌──────────────────────┬──────────────────────┬───────┼───────────────┐
             ▼                      ▼                      ▼        ▼               ▼
      ┌────────────┐        ┌─────────────┐        ┌────────────┐ ┌──────────┐ ┌──────────┐
      │ research    │        │ generator    │        │ publisher   │ │ analyzer │ │ storage  │
      │ RSS+NewsAPI │        │ LLM text +   │        │ LinkedIn API│ │ style    │ │ MongoDB  │
      │ → topics    │        │ image prompt │        │ + OAuth     │ │ guide    │ │ repos    │
      └─────┬──────┘        └──────┬───────┘        └─────────────┘ └──────────┘ └────┬─────┘
            │                      │ uses                                              │
            │                      ▼                                                   │
            │               ┌────────────┐                                            │
            └──────────────►│ llm         │ (gemini / openrouter / ollama + fallback)  │
                            └────────────┘                                            │
                                                                                       │
                          all state (cycles, topics, drafts, published) ──────────────┘
```

The **Agent** is the brain; every other package is a capability it composes.
`cmd/agent/main.go` is pure wiring: load config → connect Mongo → build the LLM
client, repos, research/generator/publisher → construct the Agent → start the
scheduler and/or web server.

---

## 3. Package-by-package

| Package | Responsibility |
|---|---|
| `cmd/agent` | Entry point; flag parsing; dependency wiring; OAuth bootstrap. |
| `cmd/regen` | One-off utility to regenerate content. |
| `internal/config` | Loads all settings from env/`.env`; validates required ones (`MONGO_URI`, provider keys). |
| `internal/ui/web/auth.go` | Sign-in-with-LinkedIn OAuth flow, sessions, `requireAuth` middleware, current-user context. |
| `internal/research` | Fetches trending items from RSS feeds + NewsAPI; dedups; converts to topic candidates. |
| `internal/analyzer` | Learns your voice from past posts; builds a style guide; stores examples. |
| `internal/generator` | Turns a topic into a `Draft`: LLM writes the post text; builds an image prompt + image URL. |
| `internal/llm` | Provider clients (`gemini`, `openrouter`, `ollama`) behind one `Client` interface, with an automatic fallback wrapper. |
| `internal/publisher` | LinkedIn OAuth2, posting, and native image/PDF upload; per-provider image generation. |
| `internal/scheduler` | Cron for the daily generate job + a poll loop that publishes due posts; self-healing. |
| `internal/storage` | MongoDB repositories for the four collections. |
| `internal/models` | Domain types: `Topic`, `Draft`, `Media`, `Published`, `Cycle`. |
| `internal/ui/cli` | Terminal draft selection (interactive, non-`-serve` runs). |
| `internal/ui/web` | The web dashboard (Go `html/template` + embedded CSS/JS), served on `:8081`. |
| `internal/agent` | Orchestrates the whole pipeline; the methods the scheduler/UI call. |

---

## 4. The core pipeline — `Agent.RunCycle()`

This is what runs when you click **Run Cycle**, on `-once`, or from the daily job.

1. **Open a cycle** — insert an `agent_cycles` record (`status=running`) so the
   run is auditable in the **Cycles** tab.
2. **Collect topics** (up to `AGENT_TOPICS_COUNT`):
   - **Custom topics first** — anything you queued in the **Topics** tab has
     priority (explicit intent, no filtering), so it's never starved by
     ever-fresh trending news.
   - **Then trending** — `research.Fetch()` pulls a larger candidate pool from
     RSS + NewsAPI. An LLM call (`relevantTrendingTopics`) keeps only items that
     fit a software-engineering audience and ranks them; a keyword heuristic is
     the graceful fallback if the LLM is unavailable.
   - **Dedup** — each candidate is checked against recent topic-title history
     (exact + near-duplicate) so the same subject is never covered twice.
3. **Load the style guide** — distilled from your past posts (analyzer), so the
   writing matches your established voice.
4. **Generate drafts** — for each topic, `generator.GenerateFromTopic()`:
   - picks a **post format** the user hasn't seen in their last 5 posts (14
     formats; stored on `Draft.Format`) plus a random hook, ending and length;
   - writes the post body honoring `BRAND_VOICE_*` (tone, max emoji, line breaks);
   - selects **3–5 hashtags** (`generator/hashtags.go`): catalog tags matched to
     the title/post plus the model's picks, with one broad reach tag;
   - asks the LLM to distil the post into a **sketchnote** (title, highlighted
     subtitle, note, doodle, diagram items, annotations, lesson, question —
     falling back to the post's own structure) and stores it as `Media.Card`.
     `internal/notecard` renders it to PNG on demand, deterministically from
     its seed, so preview and published image match.
5. **Persist** — store each `Draft`, attach it to the cycle, mark used custom
   topics as consumed.
6. **Decide**:
   - `AGENT_AUTO_PUBLISH=true` → approve + publish the first draft immediately.
   - otherwise → drafts wait in **Drafts** for you to review, or the CLI prompts
     for a selection on interactive runs.

---

## 5. Scheduling & publishing

Two loops run while `-serve` (or the default headless mode) is up:

- **Poll loop** (`pollLoop`, every few seconds) — finds drafts whose
  `scheduled_for` time has arrived and publishes them via `PublishScheduled()`.
  Short interval → posts go live within seconds of their slot.
- **Daily loop + cron** (`AGENT_SCHEDULE`, default 9 AM IST) — calls
  `EnsureDailyPost()` for the **next** publish slot (10 AM IST on the next
  `AGENT_PUBLISH_DAYS`, always at least a day ahead so there's a review window).

**Idempotence & self-healing.** `EnsureDailyPost` is safe to call from multiple
triggers (cron, startup, periodic safety check). It's guarded by an in-process
mutex *and* a DB-backed "did today already run?" check, and it skips days that
already have a scheduled post. So if the machine was asleep at the exact cron
moment, the next check catches up — the day is never silently skipped, and you
never get duplicate posts. At most **one post per calendar day**.

**How a post is published** (`publisher/client.go`) resolves the image in
preference order: (1) a pre-uploaded LinkedIn asset URN → (2) a user-uploaded
local file → (3) a Gemini/OpenAI generated image (if configured) → (4) the
handwritten sketchnote card, rendered locally → (5) the draft's `SourceURL` (an
uploaded/pasted image, or a legacy Pollinations URL). Whatever resolves first is
uploaded as native LinkedIn media and attached to the post. Uploading your own
image clears the card and AI prompt, so your image always wins.

---

## 6. Data model (MongoDB)

| Collection | Model | Holds |
|---|---|---|
| `agent_topics` | `Topic` | Trending + custom topic candidates; `used` flag; source (`rss`/`newsapi`/`custom`). |
| `agent_drafts` | `Draft` (+ embedded `Media`) | Generated posts: title, body, hashtags, status, `scheduled_for`, media. |
| `agent_published` | `Published` | Posts that went live: LinkedIn post URL, timestamp. |
| `agent_cycles` | `Cycle` | One record per run for auditing (status, draft IDs, trigger). |
| `agent_users` | `User` | One per tenant: LinkedIn identity + posting tokens + settings. |
| `agent_sessions` | `Session` | Active login sessions (token → user, expiry). |

Every content collection above carries a `user_id`; all reads filter by it.

`Draft.Status` flows: `draft → approved → (scheduled) → published` (or
`rejected`). `Media` carries `SourceURL` (preview/fallback image), `LocalPath`
(uploaded file), `LinkedInAssetURN` (after upload), plus the prompt/alt text.

---

## 7. The web dashboard (`internal/ui/web`)

Server-rendered with Go's `html/template`; CSS and JS are **embedded in the
binary** via `//go:embed`, so the single binary ships the whole UI. Pages:

- **Dashboard** — generate posts from a typed topic (live streamed over SSE),
  plus stat tiles and recent drafts/publications.
- **Drafts / Preview** — review, edit, set media, approve, schedule, reject.
- **Scheduled** — upcoming posts, with reschedule/cancel.
- **Topics** — queue custom topics (these get generation priority).
- **History** — your published posts / voice examples.
- **Cycles** — audit log of each run.

The UI is theme-aware (light/dark) and **fully responsive** — below ~820px the
nav collapses into a hamburger menu; the generate form stacks on phones.
Generated/remote images load lazily with a skeleton shimmer and a graceful
placeholder if a remote image (e.g. legacy Pollinations) fails to load.

---

## 8. Providers & failure handling

- **LLM fallback** — set `LLM_FALLBACK_PROVIDER` and if the primary provider
  errors (quota, rate limit, outage), calls transparently retry the fallback.
  The trending-relevance filter also degrades to a keyword heuristic if the LLM
  is entirely unavailable, so a cycle still produces output.
- **Images** — `handwritten` (the default) renders sketchnotes locally: free,
  no network, no rate limits. `gemini` and `openai` need a paid API plan
  (Gemini/Imagen image models return HTTP 429 on the free tier); they're asked
  for the same sketchnote, and the handwritten card is used if they fail.
  `pollinations` keeps the legacy AI-illustration URL.
- **LinkedIn API version** — `LINKEDIN_API_VERSION` (`YYYYMM`) must be a
  currently-active version. An expired value causes `426 NONEXISTENT_VERSION`;
  bump it per LinkedIn's changelog.

---

## 9. Where to change things

| I want to… | Look at |
|---|---|
| Add/remove news sources | `internal/research/rss.go` (`defaultRSSFeeds`), `newsapi.go` |
| Change the writing style/prompts | `internal/generator/prompts.go`, `text.go`, `BRAND_VOICE_*` env |
| Change image look or provider logic | `internal/notecard/` (sketchnote layout, doodles, themes), `internal/generator/notes.go`, `internal/publisher/*_image.go` |
| Adjust the schedule/cadence | `AGENT_SCHEDULE`, `AGENT_PUBLISH_DAYS`, `internal/scheduler/scheduler.go` |
| Add a web page or route | `internal/ui/web/server.go` + `templates/` |
| Swap/extend LLM providers | `internal/llm/factory.go` + a new client |
| Change what's stored | `internal/models/` + `internal/storage/` |

---

## 10. Lifecycle summary

```
 (cron 9AM IST)                     (you, in the UI)
      │                                   │
      ▼                                   ▼
 EnsureDailyPost ──► RunCycle ──► Drafts ──► approve / schedule
      │                 │                        │
      │                 └─ research → filter → generate (text + image)
      ▼                                          ▼
 scheduled_for set (10AM IST, next publish day)  │
      │                                          │
      ▼                                          ▼
 pollLoop (every few sec) ──► PublishScheduled ──► LinkedIn API ──► agent_published
```

That's the whole system: a research-driven generator, a review surface, and a
self-healing scheduler — one Go binary, one MongoDB, pluggable AI providers.
