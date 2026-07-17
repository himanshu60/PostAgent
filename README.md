# PostAgent

An autonomous **LinkedIn content agent**. It researches trending
software-engineering topics, writes on-brand LinkedIn posts (with images),
lets you review them in a web dashboard, and publishes them to LinkedIn on a
schedule — fully automatically if you want.

> Built in Go. Backed by MongoDB. Pluggable LLM (Gemini / OpenRouter / Ollama)
> and image (Pollinations / Gemini / OpenAI) providers.

---

## What it does

1. **Research** — pulls trending items from developer RSS feeds (Hacker News,
   InfoQ, The New Stack, Stack Overflow Blog, GitHub Blog, dev.to) and NewsAPI.
2. **Filter** — an LLM keeps only topics relevant to a software-engineering
   audience and drops anything already covered before (dedup by title history).
3. **Generate** — writes a LinkedIn post in your brand voice (tone, emoji limit,
   spacing) plus a matching illustration prompt/image.
4. **Review** — a polished, responsive web dashboard to preview, edit, approve,
   schedule, or reject drafts. (Or approve automatically.)
5. **Publish** — posts to LinkedIn via the official API at a scheduled time
   (default 10:00 AM IST on Mon/Wed/Fri), uploading the image as native media.

The scheduler is **self-healing**: if the process was down at the exact
scheduled moment, it catches up on the next check instead of skipping the day.

---

## Quick start (local)

**Prerequisites:** Go 1.25+, a running MongoDB, and a Gemini API key (free).

```bash
git clone https://github.com/himanshu60/PostAgent.git
cd PostAgent
cp .env.example .env         # then fill in the values (see below)
go run ./cmd/agent -serve    # web UI + scheduler on http://localhost:8081
```

Open <http://localhost:8081>, type a topic, and click **Generate Posts** — or
hit **⚡ Run Cycle** to research + generate automatically.

### Minimum config (`.env`)

```dotenv
LLM_PROVIDER=gemini
GEMINI_API_KEY=your_key_here     # https://aistudio.google.com/apikey
IMAGE_PROVIDER=pollinations      # free, no key (Gemini images need paid billing)
MONGO_URI=mongodb://localhost:27017
```

To actually publish to LinkedIn you also need a LinkedIn app and OAuth tokens —
see **[DEPLOYMENT.md](DEPLOYMENT.md)**, Step 3.

---

## Run modes

| Command | What it does |
|---|---|
| `go run ./cmd/agent -serve` | Web dashboard **and** the publish scheduler (recommended). |
| `go run ./cmd/agent` | Scheduler only (headless auto-generate + auto-publish). |
| `go run ./cmd/agent -once` | Run one research→generate cycle and exit. |
| `go run ./cmd/agent -auth` | Print the LinkedIn OAuth URL to get tokens. |
| `go run ./cmd/agent -auth -code=XXXX` | Exchange the OAuth code for access/refresh tokens. |

---

## Key configuration

| Variable | Purpose | Default |
|---|---|---|
| `LLM_PROVIDER` | `gemini` \| `openrouter` \| `ollama` | `openrouter` |
| `LLM_FALLBACK_PROVIDER` | Auto-retry provider on quota/outage | *(none)* |
| `IMAGE_PROVIDER` | `pollinations` \| `gemini` \| `openai` | `pollinations` |
| `MONGO_URI` | MongoDB connection string (**required**) | — |
| `AGENT_AUTO_PUBLISH` | Publish without manual review | `false` |
| `AGENT_PUBLISH_DAYS` | Days posts go out | `Mon,Wed,Fri` |
| `AGENT_SCHEDULE` | Cron for the daily generate check | 9 AM IST |
| `BRAND_VOICE_*` | Tone, max emoji, min line breaks | professional |

> **Images on a free plan:** keep `IMAGE_PROVIDER=pollinations`. Gemini/Imagen
> image models return HTTP 429 (quota 0) unless you enable Google Cloud billing.

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
