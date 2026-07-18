# PostAgent

A multi-tenant **LinkedIn content SaaS**. Users **sign in with LinkedIn**, and
the agent researches trending software-engineering topics, writes on-brand posts
(with images), lets each user review them in a web dashboard, and publishes them
to **their own** LinkedIn account on a schedule they control.

> Built in Go. Backed by MongoDB. Pluggable LLM (Gemini / OpenRouter / Ollama)
> and image (Pollinations / Gemini / OpenAI) providers.

## Accounts & privacy

- **Sign in with LinkedIn** — one OAuth grants both identity and posting rights.
- **Per-user isolation** — every draft, topic, and published post is private to
  the user who created it. Users never see each other's data.
- **Posts go to the poster's own account** — the scheduler publishes each post
  using that user's own LinkedIn tokens, never a shared account.
- **You approve everything** — nothing is posted unless the user schedules it.

---

## What it does

1. **Sign in** — a user authenticates with LinkedIn (OpenID Connect); their
   posting tokens are stored against their account.
2. **Research** — pulls trending items from developer RSS feeds (Hacker News,
   InfoQ, The New Stack, Stack Overflow Blog, GitHub Blog, dev.to) and NewsAPI.
3. **Filter** — an LLM keeps only topics relevant to a software-engineering
   audience and drops anything already covered before (dedup by title history).
4. **Generate** — writes a LinkedIn post in the user's brand voice (tone, emoji
   limit, spacing) plus a matching illustration.
5. **Review** — a polished, responsive dashboard to preview, edit, approve,
   schedule, or reject drafts. New users get a demo/getting-started section and
   a full in-app **Guide**.
6. **Publish** — at the scheduled time the post is published to the user's own
   LinkedIn via the official API, with the image uploaded as native media.

Generation is user-driven (manual). The background scheduler polls for due
scheduled posts and publishes each with its owner's tokens.

---

## Quick start (local)

**Prerequisites:** Go 1.25+, a running MongoDB, and a Gemini API key (free).

```bash
git clone https://github.com/himanshu60/PostAgent.git
cd PostAgent
cp .env.example .env         # then fill in the values (see below)
go run ./cmd/agent -serve    # web UI + scheduler on http://localhost:8081
```

Open <http://localhost:8081>, **Sign in with LinkedIn**, then type a topic and
click **Generate Posts**. New accounts get a demo section and an in-app **Guide**.

> The LinkedIn app must list `http://localhost:8081/auth/callback` (and your
> production URL's `/auth/callback`) as **Authorized redirect URLs**.

### Minimum server config (`.env`)

These are set once, for the server. Individual users' LinkedIn tokens come from
signing in — they are **not** put in `.env`.

```dotenv
LLM_PROVIDER=gemini
GEMINI_API_KEY=your_key_here      # https://aistudio.google.com/apikey
IMAGE_PROVIDER=pollinations       # free, no key (Gemini images need paid billing)
MONGO_URI=mongodb://localhost:27017
LINKEDIN_CLIENT_ID=...            # your LinkedIn OAuth app (for Sign in with LinkedIn)
LINKEDIN_CLIENT_SECRET=...
LINKEDIN_API_VERSION=202606
```

---

## Run modes

| Command | What it does |
|---|---|
| `go run ./cmd/agent -serve` | Web dashboard **and** the publish scheduler (recommended). |
| `go run ./cmd/agent` | Headless publish scheduler only (no web UI). |

---

## Key configuration

| Variable | Purpose | Default |
|---|---|---|
| `LLM_PROVIDER` | `gemini` \| `openrouter` \| `ollama` | `openrouter` |
| `LLM_FALLBACK_PROVIDER` | Auto-retry provider on quota/outage | *(none)* |
| `IMAGE_PROVIDER` | `pollinations` \| `gemini` \| `openai` | `pollinations` |
| `MONGO_URI` | MongoDB connection string (**required**) | — |
| `LINKEDIN_CLIENT_ID` / `LINKEDIN_CLIENT_SECRET` | Your LinkedIn OAuth app | — |
| `LINKEDIN_API_VERSION` | Active LinkedIn API version (`YYYYMM`) | `202606` |
| `PORT` | Web server port (auto on Render/Railway/etc.) | `8081` |
| `BRAND_VOICE_*` | Tone, max emoji, min line breaks | professional |

> **Images on a free plan:** keep `IMAGE_PROVIDER=pollinations`. Gemini/Imagen
> image models return HTTP 429 (quota 0) unless you enable Google Cloud billing.

> **Note:** per-user `LINKEDIN_ACCESS_TOKEN` / `PERSON_ID` env vars are no longer
> used — each user's tokens are captured at sign-in. `AGENT_AUTO_PUBLISH` is also
> retired (generation is user-driven).

---

## Deployment

For a free, always-on, auto-publishing deployment (MongoDB Atlas + a free VM +
Docker), follow **[DEPLOYMENT.md](DEPLOYMENT.md)**.

## Architecture

For how it all fits together — the pipeline, components, data model, and design
decisions — see **[PROJECT_GUIDE.md](PROJECT_GUIDE.md)**.

## Tech stack

Go · MongoDB · Gemini/OpenRouter/Ollama · Pollinations/Gemini/OpenAI ·
LinkedIn REST API · `robfig/cron` · `zap` · `gofeed`.
