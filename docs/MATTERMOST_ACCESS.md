# Mattermost access for the team (D107)

Status: v1, Nov 4, 2026. Done once by whoever has the Cloudflare domain and a login to the VM. Takes about 20 minutes.

**Why this exists.** Switch runs on the AWS VM and starts Mattermost there. People use the Mattermost desktop and phone apps, which need a server address they can reach. The VM has no inbound ports (only SSH), so the address comes from a Cloudflare Tunnel: a small program on the VM connects outward to Cloudflare, and Cloudflare forwards `https://chat.<domain>` to Mattermost. Nothing is opened in the security group.

## You need
- A domain whose DNS is on Cloudflare (a free account works) and a hostname to use, for example `chat.<domain>`.
- Access to the VM with sudo.
- The Mattermost port on the VM. Switch Console shows it under the server's **Messaging apps** row (**Sign-in details**). It is usually `8065`; use whatever Console shows.

## Where the domain comes from

- **The tunnel is free.** Cloudflare Tunnel costs nothing. **The domain is not free:** it must be a domain you own with its DNS on Cloudflare (a free Cloudflare account is enough), and Cloudflare does not hand out free domains. Cheap ones cost a few dollars a year. The GitHub Student Developer Pack has offered a free domain for a year; check the current offer before relying on it.
- **No domain at all (rehearsal only):** a Cloudflare quick tunnel (`cloudflared tunnel --url http://localhost:8065`) gives a free random `trycloudflare.com` address, but it changes on every restart, so Site URL and every person's app must be updated each time.
- **Other free option:** Tailscale (each person installs it and uses its HTTPS name). Its free plan limits the number of users, which we have not checked, so confirm the team fits.
- **In the real product the customer brings their own domain** (for example `chat.acme.com`) and exposes Mattermost however their IT exposes internal tools: Cloudflare Tunnel, a load balancer with TLS, or a VPN, with their own SSO in front if they want it. Heimdall never owns or buys a domain; it only needs the public address as a setting. Customers whose chat is Slack, Discord or Telegram need no domain for chat. Teams needs a public HTTPS name the customer controls (`TEAMS_SETUP.md`).

## Steps
1. **Create the tunnel.** In the Cloudflare dashboard open Zero Trust, then Networks, then Tunnels, and create a tunnel named `heimdall-chat`. Choose the Linux install option and copy the token. Do not paste the token in chat or commit it; save it to `/etc/heimdall/secrets/cloudflared-chat.token` on the VM (mode 0400).
2. **Install and run `cloudflared` on the VM** as a service using that token (the dashboard shows the exact command). It only connects outward.
3. **Add a public hostname** to the tunnel: `chat.<domain>` pointing to `http://localhost:8065` (or the port Console shows).
4. **Set Mattermost's Site URL.** In Mattermost System Console, Environment, Web Server, set **Site URL** to `https://chat.<domain>`. Without it, links, invites and the apps break.
5. **Tell Switch the public address.** In Switch Console edit the Mattermost connection and set **Public Url** to the same `https://chat.<domain>`, so links Switch posts open for people. Leave **Url** as the internal address Switch uses.
6. **Lock it down.** In System Console: turn off open sign-up (users join by invite only), require MFA or at least strong passwords, keep **Enable Bot Account Creation** on (Switch needs it), and keep rate limiting on. In Cloudflare add one rate-limit rule on the login path, `/api/v4/users/login`.
7. **Do not put Cloudflare Access in front.** The Mattermost phone and desktop apps cannot complete its browser login.
8. **Teams is separate.** Its tunnel routes `/api/messages` and `/api/teams/notifications` to `http://localhost:3978` (see `TEAMS_SETUP.md`).

## Check it (spike S22)
- On a phone using mobile data: install the Mattermost app, enter `https://chat.<domain>`, log in with an invite, open a room, and see an agent answer without refreshing.
- On a desktop on another network: the same.
- If login works but messages only appear after a refresh, WebSockets are blocked. Recheck the hostname points at the Mattermost port and no proxy rule strips the upgrade.

## If something breaks
- Switch and the agents keep working on the VM when the tunnel is down; only people's apps lose the connection. Restart the `cloudflared` service first.
- Rehearsal fallback: a Cloudflare quick tunnel (`cloudflared tunnel --url http://localhost:8065`) gives a random address with no domain, but the address changes on every restart, so Site URL and everyone's app must be updated each time.
- Second fallback: Tailscale on the VM and on every person's device, using its HTTPS name (phones require HTTPS).
