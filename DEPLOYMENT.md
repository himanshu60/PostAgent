# Deploying PostAgent (free, always-on, auto-publish)

This guide deploys PostAgent so it **runs 24/7**, generates LinkedIn posts on a
schedule, and **publishes them automatically** — using only free-tier services.

## How the app runs

`agent -serve` starts **two things in one process**:

1. **Web UI** on `:8081` (dashboard, drafts, topics, history).
2. **Scheduler** — a cron that generates posts before each publish day and, with
   `AGENT_AUTO_PUBLISH=true`, posts them to LinkedIn at 10:00 AM IST on
   `AGENT_PUBLISH_DAYS` (default Mon/Wed/Fri).

Because the scheduler is *inside* the process, the process must stay running.
That's why we use an **always-on VM**, not a sleeping free web tier.

## Why an Oracle Cloud "Always Free" VM

| Option | Always-on? | Truly free? | Verdict |
|---|---|---|---|
| **Oracle Cloud Always Free VM** | ✅ yes | ✅ free forever | **Recommended** |
| Google Cloud `e2-micro` free VM | ✅ yes | ✅ (1 in us-regions) | Good alternative |
| Render free web service | ❌ sleeps after 15 min | ✅ | Scheduler stops → misses posts |
| Railway | ✅ | ❌ trial credit only | Not free long-term |
| Cloud Run | scales to zero | ✅ | Cron won't fire unless min-instances≥1 (not free) |

We deploy the Docker container on a free VM and keep it alive with Docker's
restart policy.

---

## Step 1 — MongoDB Atlas (free M0 cluster)

1. Create an account at <https://www.mongodb.com/cloud/atlas/register>.
2. **Build a Database → M0 (Free)**. Pick a region near your VM.
3. **Database Access →** add a user (username + password). Save them.
4. **Network Access →** add IP `0.0.0.0/0` (allow from anywhere) — or, more
   securely, add only your VM's public IP after Step 5.
5. **Connect → Drivers →** copy the connection string. It looks like:
   ```
   mongodb+srv://USER:PASSWORD@cluster0.xxxx.mongodb.net/?retryWrites=true&w=majority
   ```
   This becomes your `MONGO_URI`. (`MONGO_DB` defaults to `linkedin_agent`.)

---

## Step 2 — Gemini API key

Your config uses `LLM_PROVIDER=gemini` and `IMAGE_PROVIDER=gemini`.

1. Go to <https://aistudio.google.com/apikey> → **Create API key**.
2. Save it as `GEMINI_API_KEY`.

> The free Gemini tier has low rate limits. Set `LLM_FALLBACK_PROVIDER` (e.g.
> `openrouter` with an `OPENROUTER_API_KEY`) so generation still works if Gemini
> throttles.

---

## Step 3 — LinkedIn app + OAuth tokens (required for auto-publish)

Auto-publish needs a valid LinkedIn access token. LinkedIn tokens are obtained
through an OAuth flow that this app automates.

1. Create an app at <https://www.linkedin.com/developers/apps> → **Create app**.
2. Request the **"Share on LinkedIn"** and **"Sign In with LinkedIn using
   OpenID Connect"** products (needed for posting + profile ID).
3. Under **Auth**, copy `Client ID` → `LINKEDIN_CLIENT_ID` and
   `Client Secret` → `LINKEDIN_CLIENT_SECRET`.
4. Add an **Authorized redirect URL**. For the token bootstrap below use:
   `http://localhost:8080/auth/callback` (matches `LINKEDIN_REDIRECT_URI`).
5. **Get tokens** (do this once, on your laptop, with the values above in a
   local `.env`):
   ```bash
   go run ./cmd/agent -auth          # prints an authorization URL
   # open the URL, authorize, you'll land on the redirect URL with ?code=XXXX
   go run ./cmd/agent -auth -code=XXXX
   ```
   It prints `LINKEDIN_ACCESS_TOKEN`, `LINKEDIN_REFRESH_TOKEN`, and
   `LINKEDIN_PERSON_ID`. Put all three in your production env (Step 4).

> **API version 426 error:** LinkedIn expires its dated API versions. If logs
> show `NONEXISTENT_VERSION`, bump `LINKEDIN_API_VERSION` to the current
> `YYYYMM` (check LinkedIn's API changelog).

---

## Step 4 — Build your production env file

On your laptop create `.env.production` (never commit it):

```dotenv
# LLM (text) — Gemini free tier works fine for text
LLM_PROVIDER=gemini
LLM_MODEL=gemini-2.5-flash
GEMINI_API_KEY=your_gemini_key
GEMINI_MODEL=gemini-2.5-flash
# Images — MUST be pollinations on the free plan. Gemini/Imagen image models
# return HTTP 429 (quota limit 0) unless you enable billing on Google Cloud.
# Pollinations is free, needs no key, and requires no quota.
IMAGE_PROVIDER=pollinations
# Optional fallback so throttling doesn't stop text generation:
# LLM_FALLBACK_PROVIDER=openrouter
# OPENROUTER_API_KEY=...

# Database
MONGO_URI=mongodb+srv://USER:PASSWORD@cluster0.xxxx.mongodb.net/?retryWrites=true&w=majority
MONGO_DB=linkedin_agent

# LinkedIn (from Step 3)
LINKEDIN_CLIENT_ID=...
LINKEDIN_CLIENT_SECRET=...
LINKEDIN_ACCESS_TOKEN=...
LINKEDIN_REFRESH_TOKEN=...
LINKEDIN_PERSON_ID=...
LINKEDIN_API_VERSION=202506

# Auto-publish ON
AGENT_AUTO_PUBLISH=true
AGENT_SCHEDULE=CRON_TZ=Asia/Kolkata 0 0 9 * * *
AGENT_PUBLISH_DAYS=Mon,Wed,Fri
AGENT_TOPICS_COUNT=5

LOG_LEVEL=info
```

> **Note on secrets:** `MONGO_URI` is required or the app won't start. Keep this
> file off git — the repo `.gitignore` already ignores `.env*`.

---

## Step 5 — Create the free VM and deploy

### 5a. Create the VM
1. Sign up at <https://www.oracle.com/cloud/free/> → **Always Free**.
2. **Compute → Instances → Create instance.** Choose an **Always Free-eligible**
   shape (e.g. `VM.Standard.A1.Flex`, 1 OCPU / 6 GB, or `VM.Standard.E2.1.Micro`).
   Image: **Ubuntu 22.04**.
3. Download the SSH private key when prompted.
4. After it boots, note the **public IP**.
5. **Networking → open port 8081:** in the instance's subnet **Security List**,
   add an ingress rule: source `0.0.0.0/0`, TCP, dest port `8081`.

### 5b. Install Docker on the VM
```bash
ssh -i your-key.pem ubuntu@<VM_PUBLIC_IP>
sudo apt-get update && sudo apt-get install -y docker.io
sudo usermod -aG docker ubuntu && exit   # re-login for group to apply
```

### 5c. Get the code and env onto the VM
```bash
ssh -i your-key.pem ubuntu@<VM_PUBLIC_IP>
git clone https://github.com/himanshu60/PostAgent.git
cd PostAgent
# copy your .env.production up (run this line from your LAPTOP, not the VM):
#   scp -i your-key.pem .env.production ubuntu@<VM_PUBLIC_IP>:~/PostAgent/.env.production
```

### 5d. Build and run (auto-restart on crash/reboot)
```bash
docker build -t postagent .
docker run -d --name postagent \
  --restart unless-stopped \
  --env-file .env.production \
  -p 8081:8081 \
  postagent
```

The image's default command is `-serve -addr=:8081`, so this one container runs
**both** the web UI and the auto-publish scheduler. `--restart unless-stopped`
brings it back after crashes and VM reboots.

---

## Step 6 — Verify it's working

```bash
docker logs -f postagent
```
You should see `Connected to MongoDB`, `LinkedIn publisher configured`, and
`Web UI server starting`. Then:

- Open `http://<VM_PUBLIC_IP>:8081` — the dashboard should load.
- Click **⚡ Run Cycle** to generate drafts immediately (don't wait for the cron).
- Watch the logs around a scheduled publish time (10:00 AM IST on a publish day)
  to confirm a post goes out. A successful publish logs the LinkedIn post URL and
  it appears under **History / Recent Publications**.

---

## Updating later

```bash
cd ~/PostAgent && git pull
docker build -t postagent . && docker rm -f postagent
docker run -d --name postagent --restart unless-stopped \
  --env-file .env.production -p 8081:8081 postagent
```

---

## Recommended hardening (optional)

- **HTTPS + real domain:** put [Caddy](https://caddyserver.com/) in front for
  automatic TLS, and restrict `:8081` to localhost so only Caddy is public.
- **Lock the UI down:** the dashboard has no auth. Either keep `:8081` closed in
  the Security List and reach it via SSH tunnel
  (`ssh -L 8081:localhost:8081 ...`), or front it with Caddy basic-auth.
- **Atlas IP allowlist:** replace `0.0.0.0/0` with your VM's public IP.
- **Token refresh:** LinkedIn access tokens expire (~60 days). The refresh token
  extends this; if publishing starts failing with auth errors, re-run the Step 3
  `-auth` flow and update the tokens.
