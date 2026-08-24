# VerticalResponse API example

An example application showing how the VerticalResponse delegated-app API works:
the OAuth2 authorization-code flow end to end, then real calls against the API
with the token it returns.

```
go install github.com/VerticalResponse/vr2_api_example@latest
vr2_api_example
```

Then open <http://localhost:9190>. It points at production by default.

## What it asks for

The **Client ID** and **Client secret** of a **Delegated app**, which you
register under **API Applications** in your account settings. A *Direct (API
key)* application will not do: it can call the API as you, but it cannot ask
another user for access, which is the whole point of this flow.

Set its Redirect URI to `http://localhost:9190/callback`. The provider compares
that string for equality, with no wildcards and no tolerance for a trailing
slash, and a mismatch is refused on the provider rather than redirected back —
so the app never sees it and cannot tell you what went wrong.

The secret is held in memory for the browser session and nowhere else.

## What it calls

| | Host | |
|---|---|---|
| `GET /oauth/authorize` | provider | The consent screen. A browser redirect, not a request this app makes — it needs the approving user's own session. |
| `POST /api/v1/authorize` | gateway | Code for a token, and later refresh for a new one. Back-channel, carries the secret. |
| `DELETE /api/v1/oauth/revoke` | provider | Ends the authorization. Refresh afterwards fails with `400 invalid_grant`. |
| `GET /api/v1/status/is_alive` | gateway | Needs no token, so it answers before the flow has been run. |
| `GET /api/v1/contacts` | gateway | Collection: `url`, `count`, `items`. |
| `GET /api/v1/contacts/{id}` | gateway | Single resource: `attributes` and `links` instead. |
| `GET /api/v1/lists` | gateway | |
| `GET /api/v1/lists/{id}/contacts` | gateway | An id belonging to no list answers 404, not an empty collection. |

Two hosts, and the split is not arbitrary. The gateway serves `/api/v1/*` and is
what validates the token signature, so everything that can go through it does.
The consent screen is a server-rendered page rather than an API route, and
revoke is an `/api/v1` path the gateway does not list — send it there and it
404s. Those two go to the provider directly.

Every request is a read. Writes are a `POST` of the same JSON envelope to the
same path, and the API names the offending field under `failures` when it
rejects one.
