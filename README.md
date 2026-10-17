# PostAgent

A multi-tenant **LinkedIn content SaaS**. Users **sign in with LinkedIn**, and
the agent researches trending software-engineering topics, writes on-brand posts
(with images), lets each user review them in a premium web dashboard, and — if
they opt in — publishes them to **their own** LinkedIn account on a schedule.

> Built in Go. Backed by MongoDB. Pluggable LLM (Gemini / OpenRouter / Ollama)
> and image (free handwritten sketchnotes / Gemini / OpenAI / Pollinations)
> providers. Deployed on Render.
>
> **Live:** <https://postagent-9fl6.onrender.com>

## Accounts & privacy

- **Sign in with LinkedIn** — one OAuth grants both identity and posting rights.
- **Per-user isolation** — every draft, topic, and published post is private to
  the user who created it. Users never see each other's data.
- **Posts go to the poster's own account** — the scheduler publishes each post
  using that user's own LinkedIn tokens, never a shared account.
- **You're always in control** — nothing is posted unless you schedule it, and
  automatic posting is strictly opt-in.

---

## Features

- 🔐 **Sign in with LinkedIn** (OpenID Connect) — no passwords; posting tokens
  captured at login.
- 🔎 **Trend research** — pulls from developer RSS (Hacker News, InfoQ, The New
  Stack, Stack Overflow Blog, GitHub Blog, dev.to) and NewsAPI, filtered by an
  LLM to a software-engineering audience and deduped against your history.
- ✍️ **Human-sounding, varied posts** — 14 post formats (story, hot take,
  how-to, myth vs reality, checklist, mistakes I made, …). Each user's next post
  avoids their last 5 formats, and hooks, endings and length are randomized, so
  the feed never looks templated. AI-cliché phrases are banned, and stories or
  opinions skip the bold title line.
- 🖍️ **Free handwritten sketchnote images** — every post gets a hand-drawn
  page (paper on a desk with pens, notebook, graph paper, whiteboard or
  chalkboard) with the post's title, highlighted subtitle, a doodle, a
  labelled diagram, annotations, the lesson, a question and hashtags. Real
  handwriting fonts, so every word is legible; rendered locally, no API, no cost.
  "Try another style" re-rolls the look on any draft.
- #️⃣ **Reach-friendly hashtags** — 3–5 per post: popular topic tags matched to
  the post (e.g. #Kubernetes, #SystemDesign) plus one broad tag, instead of a
  spammy wall of 15.
- Per-user topic shuffling means two users never get the same post.
- 🎯 **Your topics & designation** — set your role and pick/add the topics you
  post about (Settings). Auto-generated posts are drawn from *your* interests
  and written from your role's perspective.
- 🤖 **Opt-in auto-post** — enable it and the agent researches, writes, and
  publishes to your LinkedIn every publish day (Mon/Wed/Fri, 10 AM IST),
  hands-free. Stop it anytime. Self-healing scheduler catches up after downtime.
- 🗂️ **Review dashboard** — preview, edit, approve, schedule, or reject drafts;
  upload/replace the image (stored in MongoDB, so it survives restarts).
- 📘 **In-app Guide** — how it works + a complete "deploy your own instance"
  walkthrough with every step and environment variable.
- 💎 **Premium UI** — Linear/Vercel-style design, light/dark themes, responsive,
  premium confirm modals and toast notifications.

---

## Quick start (local)

**Prerequisites:** Go 1.25+, a running MongoDB, a Gemini API key (free), and a
LinkedIn OAuth app.

```bash
git clone https://github.com/himanshu60/PostAgent.git
cd PostAgent
cp .env.example .env         # fill in the values below
go run ./cmd/agent -serve    # web UI + scheduler on http://localhost:8081
```

Open <http://localhost:8081>, **Sign in with LinkedIn**, then type a topic and
click **Generate Posts** — or set your topics in **Settings** and enable
auto-post.

> Your LinkedIn app must list `http://localhost:8081/auth/callback` (and your
> production URL's `/auth/callback`) under **Authorized redirect URLs**, with
> the **Sign In with LinkedIn (OpenID Connect)** and **Share on LinkedIn**
> products enabled.

### Minimum server config (`.env`)

Set once, for the server. Users' LinkedIn tokens come from signing in — they are
**not** put in `.env`.

```dotenv
LLM_PROVIDER=gemini
GEMINI_API_KEY=your_key_here      # https://aistudio.google.com/apikey
IMAGE_PROVIDER=handwritten        # free sketchnotes, no key (Gemini images need paid billing)
MONGO_URI=mongodb://localhost:27017
LINKEDIN_CLIENT_ID=...            # your LinkedIn OAuth app
LINKEDIN_CLIENT_SECRET=...
LINKEDIN_API_VERSION=202606       # must be a currently-active YYYYMM
```

---

## Run modes

| Command | What it does |
|---|---|
| `go run ./cmd/agent -serve` | Web dashboard **and** the scheduler (recommended). |
| `go run ./cmd/agent` | Headless scheduler only (publishes due posts; runs the daily auto-generate for opted-in users). |

---

## Key configuration

| Variable | Purpose | Default |
|---|---|---|
| `LLM_PROVIDER` | `gemini` \| `openrouter` \| `ollama` | `openrouter` |
| `LLM_FALLBACK_PROVIDER` | Auto-retry provider on quota/outage | *(none)* |
| `GEMINI_API_KEY` | Required when provider/fallback is Gemini | — |
| `IMAGE_PROVIDER` | `handwritten` \| `gemini` \| `openai` \| `pollinations` | `handwritten` |
| `MONGO_URI` | MongoDB connection string (**required**) | — |
| `LINKEDIN_CLIENT_ID` / `LINKEDIN_CLIENT_SECRET` | Your LinkedIn OAuth app | — |
| `LINKEDIN_API_VERSION` | Active LinkedIn API version (`YYYYMM`) | `202606` |
| `AGENT_SCHEDULE` | Cron for the daily auto-generate check | 9 AM IST |
| `AGENT_PUBLISH_DAYS` | Days posts go live | `Mon,Wed,Fri` |
| `AGENT_TOPICS_COUNT` | Posts generated per manual cycle | `5` |
| `PORT` | Web server port (auto on Render/Railway/etc.) | `8081` |
| `BRAND_VOICE_*` | Tone, max emoji, min line breaks | professional |

> **Images on a free plan:** use `IMAGE_PROVIDER=handwritten` (the default).
> Gemini/Imagen image models return HTTP 429 (quota 0) unless you enable Google
> Cloud billing; with billing on, `gemini`/`openai` are asked for the same
> sketchnote, and the handwritten one is the preview and fallback.
> `pollinations` keeps the old AI-illustration images.

> Per-user `LINKEDIN_ACCESS_TOKEN` / `PERSON_ID` and `AGENT_AUTO_PUBLISH` env
> vars are **not** used — tokens are captured at sign-in, and auto-posting is
> toggled per user in **Settings**.

---

## Using it

1. **Sign in** with LinkedIn.
2. **Settings** → set your designation and pick/add your topics.
3. **Generate** posts manually (type a topic → *Generate Posts*), review in
   **Drafts**, then **Schedule** or **Publish**.
4. Or **Settings → Automatic posting → Enable** to have the agent create and
   publish posts for you automatically. Stop it anytime.

---

## Deployment

For a free, always-on deployment (MongoDB Atlas + Render + UptimeRobot keep-
alive), follow **[DEPLOYMENT.md](DEPLOYMENT.md)**. The in-app **Guide** page has
the same walkthrough for end users who want their own instance.

## Architecture

For how it all fits together — auth/multi-tenancy, the pipeline, per-user
publishing, components, and data model — see **[PROJECT_GUIDE.md](PROJECT_GUIDE.md)**.

## Tech stack

Go · MongoDB · LinkedIn OAuth2/OpenID Connect · Gemini/OpenRouter/Ollama ·
`fogleman/gg` (sketchnote renderer) · Gemini/OpenAI · `robfig/cron` · `zap` ·
`gofeed` · Docker · Render.
