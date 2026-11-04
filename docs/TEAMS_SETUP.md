# Connecting Microsoft Teams (admin guide)

**Audience:** the IT or Microsoft 365 admin at a company that installs Heimdall and wants its change rooms in Teams instead of Mattermost. This is a one-time manual setup done by that person; Heimdall only needs `chat.bridge: teams` in its policy afterwards (step 9). Our README links to this page.

Heimdall needs no code change for Teams. Teams is one more Switch chat connection. Steps are based on Switch's own guide (`docs/official/deploy/messaging-apps/microsoft-teams.md` and `docs/old/bridges/TEAMS_SETUP.md` in the Switch checkout).

**Time:** about an hour if you are the tenant admin; longer if you wait on one.
**Cost:** a domain (about $10 a year) if you do not have one. Cloudflare Tunnel, the Azure Bot and the app registration are free for this use (Teams is a standard Bot channel); a Microsoft 365 tenant is needed (a Business trial works for the hackathon).

## What you need first

| Need | Why |
|---|---|
| A Microsoft 365 tenant where you (or a teammate) are **Global Administrator** | Graph permissions need tenant-wide admin consent, and the Teams app must be installed into a team |
| An Azure subscription (free account is fine) | To create the Azure Bot resource |
| A domain whose DNS is on Cloudflare | Teams is push-based: Microsoft calls Switch, so Switch's Teams listener needs a **public HTTPS** name. A tailnet or IAP forward cannot work (V43) |
| Admin on the Switch server | Console set that up for us already |

Pick the public hostname now and do not change it later, for example `teams.<yourdomain>`. It goes into Azure, the tunnel and Switch.

## Step 1. Public HTTPS for the Teams listener (Cloudflare named tunnel)

Switch's Teams listener runs on port **3978** (not the main Switch port). Only this listener is exposed; Heimdall itself stays private (D64).

On the VM:

```bash
cloudflared tunnel login
```

```bash
cloudflared tunnel create switch-teams
```

```bash
cloudflared tunnel route dns switch-teams teams.<yourdomain>
```

Create `/etc/cloudflared/config.yml`:

```yaml
tunnel: switch-teams
credentials-file: /root/.cloudflared/<tunnel-id>.json
ingress:
  - hostname: teams.<yourdomain>
    service: http://localhost:3978
  - service: http_status:404
```

```bash
sudo cloudflared service install && sudo systemctl enable --now cloudflared
```

Both `/api/messages` and `/api/teams/notifications` reach the listener through this one hostname.

## Step 2. App registration (Microsoft Entra ID)

Azure portal → **Microsoft Entra ID → App registrations → New registration** (single tenant).

Write down:
- **Application (client) ID** → `App Id`
- **Directory (tenant) ID** → `Tenant Id`

Then **Certificates & secrets → New client secret**. Copy the **Value** column, not the Secret ID (the most common mistake). Note the expiry date.

## Step 3. Graph permissions (application, not delegated)

**API permissions → Add a permission → Microsoft Graph → Application permissions**, add all six, then **Grant admin consent**:

| Permission | Without it |
|---|---|
| `ChannelMessage.Read.All` | Agents only hear messages that @mention the bot |
| `Channel.Create` | Switch cannot create a channel for a new room |
| `Channel.ReadBasic.All` | Switch cannot tell standard from private channels after a restart |
| `TeamMember.ReadWrite.All` | People are not added to standard channels |
| `ChannelMember.ReadWrite.All` | People are not added to private channels |
| `User.ReadBasic.All` | Nobody can link their Teams account; everyone is an opaque id |

A permission added but not consented behaves as missing. If channel capture is refused even with consent, the tenant may need Microsoft's "protected APIs for Teams" access; check Microsoft Graph's current docs.

## Step 4. Azure Bot resource

Azure portal → **Create a resource → Azure Bot**:
- **Use existing app registration**: the one from step 2. Do not let the form create a new one; afterwards check **Configuration → Microsoft App ID** matches step 2.
- **Messaging endpoint:** `https://teams.<yourdomain>/api/messages`
- **Channels → Microsoft Teams → enable.**

## Step 5. Encryption certificate (for reading channel messages)

```bash
openssl req -x509 -newkey rsa:2048 -nodes -days 365 -subj "/CN=switch-teams" -keyout teams-key.pem -out teams-cert.pem
```

Pick a stable certificate id, for example `switch-teams-1`. Keep `teams-key.pem` private (it goes only into the Switch form).

## Step 6. Teams app package and install

Switch ships the manifest template (see the Switch checkout, `TEAMS_SETUP.md` section 1.5). Fill in the bot's App Id and your hostname, zip `manifest.json` with the two icons, then:
- **Teams admin center → Teams apps → Manage apps → Upload new app**, or Teams → **Apps → Manage your apps → Upload an app** if custom uploads are allowed.
- Add the app to the team you will use.
- Copy the **team id**: in Teams, the team's **Get link to team**; the `groupId` in that link is the team id.

## Step 7. Connect in Switch Console

Server **Home → Messaging apps → Connect → Microsoft Teams**. Fill in:

| Field | Value |
|---|---|
| Name | your tenant name |
| App Id | step 2 |
| App Password | step 2 secret **value** |
| Tenant Id | step 2 |
| Team Id | step 6 |
| Public Base Url | `https://teams.<yourdomain>` |
| Encryption Certificate Id | `switch-teams-1` |
| Encryption Public Certificate | contents of `teams-cert.pem` |
| Encryption Private Key | contents of `teams-key.pem` |
| Client State | output of `openssl rand -hex 32` (required; Switch checks it on every notification) |

Select **Connect**, then **This is me** to link your Teams account. Every teammate links their own account too (V7), or agents treat them as strangers.

## Step 8. Check it works

1. The connection shows under **Messaging apps** with no error.
2. Add the Switch app to a channel; the channel appears under **Your Rooms**.
3. Post a message that does **not** tag any agent, then `!agents-status`. An agent answering an untagged message proves channel capture works.

## Step 9. Point Heimdall at Teams

In `heimdall.yaml` set `chat.bridge: teams` (the connection name), so new change rooms are created as Teams channels. Existing Mattermost rooms stay where they are.

## What is different in Teams

- Agent messages appear as cards headed with the agent's name.
- Only `!` commands work (no native slash commands). Use `!heimdall …` for Heimdall commands (for example `!heimdall uphold F-3`); Teams can intercept messages that start with `/`.
- Agent @-mentions arrive as plain text and do not notify anyone. Heimdall therefore also posts questions and decision cards as replies in the thread you follow; turn on notifications for that channel.
- Files are not relayed yet; text is.
- No one-to-one chats with agents; rooms are channels.
- One Teams connection per listener port.
