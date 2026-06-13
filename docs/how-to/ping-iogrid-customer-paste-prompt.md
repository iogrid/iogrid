# ping → iogrid: customer paste-prompt (switch off SSH to the API)

Paste the block below into the **ping** session. It switches ping's iOS builds
from the direct-SSH-to-Hatice's-Mac hack to submitting through the iogrid
build-gateway API as a real paying ($GRID) customer. Refs iogrid #700 / #757.

---

```
You are the ping engineer. Until now, ping's iOS builds ran by SSHing directly
into a shared Mac and invoking xcodebuild by hand. That bypass is DEPRECATED.
iogrid now exposes a turnkey iOS-build CI product, and ping is its first
customer. Switch ping's build path to the iogrid API. ZERO SSH from here on.

WHAT IOGRID GIVES YOU
- A REST API at https://build.iogrid.org. You submit a build with one POST +
  an API key; a real macOS provider runs your xcodebuild; you poll status and
  download the artifact. The provider is paid in devnet $GRID. You never touch
  the Mac.

CREDENTIAL
- You need an iogrid customer API key. Ask the iogrid operator/session for one
  (it is a build-gateway-issued key bound to a workspace). Export it:
      export IOGRID_API_KEY=<key>
  Never commit it. Send it as `Authorization: Bearer $IOGRID_API_KEY`.

SUBMIT A BUILD (the exact ping build command that already works natively)
  curl -sS -X POST https://build.iogrid.org/v1/builds \
    -H "Authorization: Bearer $IOGRID_API_KEY" \
    -H 'Content-Type: application/json' \
    --data '{
      "git_url": "https://github.com/ping/ping.git",
      "git_ref": "main",
      "build_command": "xcodebuild -workspace Ping.xcworkspace -scheme Ping -destination \"platform=iOS Simulator,name=iPhone 16 Pro\" -derivedDataPath /tmp/ping-build build"
    }'
  -> 202 with {"build_id": "...", "status": "dispatched", ...}. Save build_id.

WATCH / POLL / DOWNLOAD
  # live logs (SSE):
  curl -sS -N https://build.iogrid.org/v1/builds/$BUILD_ID/logs \
       -H "Authorization: Bearer $IOGRID_API_KEY"
  # poll until terminal (succeeded|failed|timed_out|cancelled|rejected):
  curl -sS https://build.iogrid.org/v1/builds/$BUILD_ID \
       -H "Authorization: Bearer $IOGRID_API_KEY" | jq .status
  # on success, have your build_command zip the .app, then download:
  PRESIGN=$(curl -sS https://build.iogrid.org/v1/builds/$BUILD_ID/artifacts/Ping.app.zip \
       -H "Authorization: Bearer $IOGRID_API_KEY" | jq -r .url)
  curl -sS -o Ping.app.zip "$PRESIGN"

OR JUST USE THE WRAPPER (clone iogrid, or copy the script):
  IOGRID_API_KEY=<key> ./scripts/submit-ios-build.sh \
    --repo https://github.com/ping/ping.git --ref main \
    --cmd 'xcodebuild -workspace Ping.xcworkspace -scheme Ping -destination "platform=iOS Simulator,name=iPhone 16 Pro" -derivedDataPath /tmp/ping-build build' \
    --artifact Ping.app.zip

GOTCHAS YOU ALREADY KNOW (carry them into the build_command, not SSH workarounds)
- The Xcode-26 / newer-clang `fmt` consteval snag (`call to consteval function
  'fmt::basic_format_string...'`) bit ping's first native build. Whatever flag/
  patch fixed it natively, bake it into git (the provider builds from your repo
  + ref), NOT into a Mac you log into.
- Pin Xcode if needed: GET https://build.iogrid.org/v1/xcode-versions, then add
  "xcode_version":"<ver>" to the submit body. Builds run on the provider's Xcode.
- Simulator destination is fine for CI (no signing). For a signed build, add
  "signing_team_id":"<TEAMID>".

WHAT NOT TO DO
- Do NOT SSH to the Mac. Do NOT ask for Mac credentials. Do NOT use the old
  ~/ping-builds/native-ios-build.sh hand-off recipe. The API is the product.
- If a build sits in "dispatched" forever, a macOS provider isn't connected —
  flag the iogrid session (provider supply), don't fall back to SSH.

COORDINATION
- Coordinate with the iogrid session via chepherd directly (not through the
  human). If you can't reach iogrid over chepherd, report the transport gap.

Confirm the switch by running one real build through the API and pasting the
terminal status + the downloaded artifact's `ls -lh`.
```

---

## For the iogrid operator: issue ping a key

Until per-customer billing-svc key issuance is exposed in the console, the
build-gateway accepts a single static key (`BUILD_GATEWAY_STATIC_API_KEY` in the
`build-gateway-secrets` Secret, bound to the workspace/user in
`build-gateway-config`). Hand that key to ping as `IOGRID_API_KEY`, or mint a
dedicated key once billing-svc `CreateApiKey` is wired to the customer console
(`iogrid.billing.v1.ApiKeyService/CreateApiKey`). Never paste the key value into
git, chat logs, or the issue tracker.
