# Deploying PostAgent — free, always-on, auto-publish (Render)

This guide deploys PostAgent on **Render's free tier** so it runs 24/7,
generates posts on a schedule, and **publishes to LinkedIn automatically** —
at $0/month. It also lists every API and environment variable the app uses.

- **Recommended path:** Render (this guide). Simplest — deploy straight from
  GitHub, no server to manage.
- **Most robust free path:** an Oracle Cloud Always Free VM (never sleeps, no
  keep-alive needed). See [§8](#8-alternative-oracle-cloud-always-free-vm).

---

## 0. The one Render caveat (and the free fix)

`agent -serve` runs the web UI **and** the auto-publish scheduler in one
process, so the process must stay alive. Render's **free** web service **sleeps
after 15 minutes with no traffic** — which would pause the scheduler and miss
scheduled posts.

**Free fix:** a free [UptimeRobot](https://uptimerobot.com) monitor pings the
app every 5 minutes so it never sleeps (§6). That keeps the whole thing free
and always-on. (If you'd rather not rely on that, use the Oracle VM in §7, or
Render's paid Starter plan at ~$7/mo which never sleeps.)

---

## 1. APIs used and what each is for

| API / service | Used for | Required? | Where to get it |
|---|---|---|---|
| **MongoDB Atlas** | Stores topics, drafts, published posts, cycles | ✅ Yes | <https://www.mongodb.com/cloud/atlas/register> |
| **Google Gemini API** | Writes the post text (LLM) | ✅ Yes (if `LLM_PROVIDER=gemini`) | <https://aistudio.google.com/apikey> |
| **LinkedIn API (OAuth2 + REST)** | Publishes posts + uploads images | ✅ Yes (for publishing) | <https://www.linkedin.com/developers/apps> |
| **Pollinations.ai** | Generates the post image (free, keyless) | ✅ Yes (default image provider) | *no signup* |
| OpenRouter | Optional LLM provider / fallback | ⬜ Optional | <https://openrouter.ai/keys> |
| Ollama | Optional local LLM (self-hosted) | ⬜ Optional | <https://ollama.com> |
| OpenAI | Optional image provider | ⬜ Optional | <https://platform.openai.com/api-keys> |
| NewsAPI | Extra trending-topic source | ⬜ Optional | <https://newsapi.org> |

---

## 2. Complete environment variable reference

Set these in Render's **Environment** tab (§5). `✱` = required.

### LLM (text generation)
| Variable | Example / default | Notes |
|---|---|---|
| `LLM_PROVIDER` | `gemini` | `gemini` \| `openrouter` \| `ollama` |
| `LLM_MODEL` | `gemini-2.5-flash` | Model for the chosen provider |
| `GEMINI_API_KEY` ✱ | `AIza...` | Required when provider/fallback is gemini |
| `GEMINI_MODEL` | `gemini-2.5-flash` | Text model |
| `LLM_FALLBACK_PROVIDER` | `openrouter` | Auto-retry if primary errors (optional) |
| `OPENROUTER_API_KEY` | `sk-or-...` | Only if using OpenRouter |

### Images
| Variable | Example / default | Notes |
|---|---|---|
| `IMAGE_PROVIDER` ✱ | `pollinations` | **Keep `pollinations` on free plans.** `gemini`/`openai` need paid billing (free Gemini image tier returns HTTP 429). |
| `OPENAI_API_KEY` | `sk-...` | Only if `IMAGE_PROVIDER=openai` |

### Database
| Variable | Example / default | Notes |
|---|---|---|
| `MONGO_URI` ✱ | `mongodb+srv://user:pass@cluster0.xxx.mongodb.net/...` | From Atlas (§3). Required or app won't start. |
| `MONGO_DB` | `linkedin_agent` | Database name |

### LinkedIn (publishing)
| Variable | Notes |
|---|---|
| `LINKEDIN_CLIENT_ID` ✱ | From your LinkedIn app |
| `LINKEDIN_CLIENT_SECRET` ✱ | From your LinkedIn app |
| `LINKEDIN_ACCESS_TOKEN` ✱ | From the OAuth flow (§4) |
| `LINKEDIN_REFRESH_TOKEN` ✱ | From the OAuth flow (§4) |
| `LINKEDIN_PERSON_ID` ✱ | From the OAuth flow (§4) |
| `LINKEDIN_REDIRECT_URI` | `http://localhost:8080/auth/callback` (used during §4) |
| `LINKEDIN_API_VERSION` | `202606` — **must be a currently-active `YYYYMM`**; expired → `426` error |

### Scheduling & behavior
| Variable | Default | Notes |
|---|---|---|
| `AGENT_AUTO_PUBLISH` | `false` → set **`true`** | Publish without manual review |
| `AGENT_SCHEDULE` | `CRON_TZ=Asia/Kolkata 0 0 9 * * *` | Daily generate check |
| `AGENT_PUBLISH_DAYS` | `Mon,Wed,Fri` | Days posts go live (10 AM IST) |
| `AGENT_TOPICS_COUNT` | `5` | Topics researched per cycle |
| `BRAND_VOICE_TONE` | `professional` | Writing tone |
| `BRAND_VOICE_MAX_EMOJI` | `3` | Emoji cap |
| `BRAND_VOICE_MIN_LINE_BREAKS` | `3` | Minimum spacing |
| `LOG_LEVEL` | `info` | Log verbosity |
| `PORT` | *(Render sets this)* | The app auto-binds to it — **don't hard-code** |

---

## 3. MongoDB Atlas (free M0)

1. Sign up → **Build a Database → M0 (Free)**, pick a region.
2. **Database Access →** create a user + password.
3. **Network Access →** add `0.0.0.0/0` (Render's IPs are dynamic on free tier).
4. **Connect → Drivers →** copy the connection string; that's `MONGO_URI`.

---

## 4. LinkedIn app + OAuth tokens

1. Create an app at <https://www.linkedin.com/developers/apps>.
2. Add products: **"Share on LinkedIn"** and **"Sign In with LinkedIn using
   OpenID Connect"**.
3. **Auth →** copy `Client ID`/`Client Secret`; add redirect URL
   `http://localhost:8080/auth/callback`.
4. On your laptop, put `LINKEDIN_CLIENT_ID`/`SECRET` + `MONGO_URI` +
   `GEMINI_API_KEY` in a local `.env`, then:
   ```bash
   go run ./cmd/agent -auth            # opens/prints an authorization URL
   # authorize in the browser → you're redirected to ...?code=XXXX
   go run ./cmd/agent -auth -code=XXXX # prints the 3 token values
   ```
5. Copy the printed `LINKEDIN_ACCESS_TOKEN`, `LINKEDIN_REFRESH_TOKEN`, and
   `LINKEDIN_PERSON_ID` — you'll paste them into Render (§5).

---

## 5. Deploy on Render

1. Push your code to GitHub (already done: `github.com/himanshu60/PostAgent`).
2. Go to <https://dashboard.render.com> → **New → Web Service** → connect the
   repo.
3. Render detects the **Dockerfile** — keep the default. Settings:
   - **Instance type:** Free
   - **Region:** closest to your Atlas region
   - No start command needed (the Dockerfile runs `agent -serve`, which binds
     `$PORT` automatically).
4. **Environment →** add every `✱` variable from §2 (and any optional ones).
   Set `AGENT_AUTO_PUBLISH=true`, `IMAGE_PROVIDER=pollinations`. Do **not** set
   `PORT` — Render provides it.
5. **Create Web Service.** Render builds the image and deploys. Watch the logs
   for `Connected to MongoDB`, `LinkedIn publisher configured`, and
   `Web UI server starting`.
6. Your app is live at `https://<your-service>.onrender.com`.

---

## 6. Keep it awake (free, so the scheduler runs 24/7)

1. Sign up at <https://uptimerobot.com> (free).
2. **Add New Monitor → HTTP(s)**, URL = your Render URL, interval = **5 min**.
3. That steady traffic prevents Render's free tier from sleeping, so the
   scheduler keeps firing and posts publish on time.

---

## 7. Verify it's working

- Open `https://<your-service>.onrender.com` — dashboard loads.
- Click **⚡ Run Cycle** → drafts appear (don't wait for the daily cron).
- Around a publish slot (10 AM IST on a publish day), check logs / the
  **History** tab for a published post with its LinkedIn URL.

> **If publishing fails with `426 NONEXISTENT_VERSION`:** bump
> `LINKEDIN_API_VERSION` to the current `YYYYMM` and redeploy.

---

## 8. Alternative: Oracle Cloud Always Free VM

If you prefer a server that never sleeps (no keep-alive needed) and is free
forever:

1. Create an **Always Free** VM (Ubuntu 22.04) at
   <https://www.oracle.com/cloud/free/>; open port `8081` in its Security List.
2. Install Docker: `sudo apt-get update && sudo apt-get install -y docker.io`.
3. `git clone` the repo, `scp` up a `.env.production` with the §2 variables.
4. Build and run always-on:
   ```bash
   docker build -t postagent .
   docker run -d --name postagent --restart unless-stopped \
     --env-file .env.production -p 8081:8081 postagent
   ```
5. Visit `http://<VM_IP>:8081`. Update later with
   `git pull && docker build -t postagent . && docker rm -f postagent && docker run ...`.

**Hardening (either platform):** the dashboard has no login — front it with
Caddy (auto-HTTPS) + basic-auth, and tighten the Atlas IP allowlist once you
know your egress IP.
