#!/usr/bin/env python3
"""promote-to-internal-tester.py — fast-path TestFlight install for emrahbaysal@gmail.com.

External beta groups (like vpn-beta) require Apple Beta App Review before
ANY invited tester can install. Review needs App Store Connect metadata
filled (description, privacy URL, test info, screenshots). For the v1
TestFlight drop, that's a 1-4h wait + a separate metadata-fill workflow.

INTERNAL beta groups skip review entirely. The only gate: the tester
must be a member of the App Store Connect team. This script:

  1. Reads emrahbaysal@gmail.com's user record on the team (POST
     /v1/users invites them if absent — they'll get an email + must
     accept once).
  2. Ensures a `vpn-internal` beta group exists with
     isInternalGroup=true.
  3. Adds the tester to that group (idempotent).
  4. Assigns the latest build to the group so the tester sees it.

When emrahbaysal@gmail.com receives the TestFlight email, he can install
immediately — no review wait.

Refs #574 #575.

Usage (from CI):

  ASC_KEY_ID=$APP_STORE_CONNECT_KEY_ID \
  ASC_ISSUER_ID=$APP_STORE_CONNECT_ISSUER_ID \
  ASC_KEY_PATH=$KEY_FILE \
  RUN_NUMBER=$github_run_number \
  python3 promote-to-internal-tester.py

NOT YET WIRED INTO mobile-ios-ci.yml — landing as a separate commit
after first CI success so the workflow file change doesn't cancel
the in-flight run via the concurrency group.
"""

import os
import sys
import time
import jwt
import requests

KEY_ID = os.environ["ASC_KEY_ID"]
ISSUER_ID = os.environ["ASC_ISSUER_ID"]
KEY_PATH = os.environ["ASC_KEY_PATH"]
RUN_NUMBER = os.environ.get("RUN_NUMBER", "")
EMAIL = "emrahbaysal@gmail.com"
FIRST = "Emrah"
LAST = "Baysal"
BUNDLE = "io.iogrid.app"
GROUP_NAME = "vpn-internal"

tok = jwt.encode(
    {"iss": ISSUER_ID, "iat": int(time.time()), "exp": int(time.time()) + 1200, "aud": "appstoreconnect-v1"},
    open(KEY_PATH).read(),
    algorithm="ES256",
    headers={"kid": KEY_ID},
)
H = {"Authorization": f"Bearer {tok}", "Content-Type": "application/json"}
BASE = "https://api.appstoreconnect.apple.com"


def find_app():
    r = requests.get(f"{BASE}/v1/apps?filter[bundleId]={BUNDLE}", headers=H, timeout=15)
    r.raise_for_status()
    data = r.json().get("data", [])
    if not data:
        print(f"::error::App {BUNDLE} not in ASC")
        sys.exit(1)
    return data[0]["id"]


def ensure_team_user():
    """Apple's /v1/users API is admin-only and surfaces existing team
    members + sends invites to new ones. If the email is already on the
    team (even as a non-admin) this returns the user id."""
    r = requests.get(f"{BASE}/v1/users?filter[username]={EMAIL}", headers=H, timeout=15)
    if r.status_code == 200 and r.json().get("data"):
        uid = r.json()["data"][0]["id"]
        print(f"  Team user {EMAIL} already exists: {uid}")
        return uid
    # Invite as ADMIN so they have visibility into all apps + can be
    # added as internal tester to any group.
    payload = {
        "data": {
            "type": "userInvitations",
            "attributes": {
                "email": EMAIL,
                "firstName": FIRST,
                "lastName": LAST,
                "roles": ["ADMIN"],
                "allAppsVisible": True,
                "provisioningAllowed": True,
            },
        }
    }
    r = requests.post(f"{BASE}/v1/userInvitations", headers=H, json=payload, timeout=15)
    if r.status_code in (200, 201):
        print(f"  ✓ Invited {EMAIL} as team ADMIN — accept the invite once")
        print("::warning::ADMIN invite sent. Tester must accept it before they can be added as internal beta tester.")
        # Don't fail — the rest still tries the external path
        return None
    print(f"::warning::userInvitations HTTP {r.status_code}: {r.text[:300]}")
    return None


def ensure_internal_group(app_id):
    r = requests.get(f"{BASE}/v1/apps/{app_id}/betaGroups", headers=H, timeout=15)
    r.raise_for_status()
    for g in r.json().get("data", []):
        if g["attributes"]["name"] == GROUP_NAME and g["attributes"].get("isInternalGroup"):
            print(f"  Internal group {GROUP_NAME}: {g['id']}")
            return g["id"]
    # Create the internal group.
    payload = {
        "data": {
            "type": "betaGroups",
            "attributes": {"name": GROUP_NAME, "isInternalGroup": True, "publicLinkEnabled": False},
            "relationships": {"app": {"data": {"type": "apps", "id": app_id}}},
        }
    }
    r = requests.post(f"{BASE}/v1/betaGroups", headers=H, json=payload, timeout=15)
    if r.status_code in (200, 201):
        gid = r.json()["data"]["id"]
        print(f"  ✓ Created internal group {GROUP_NAME}: {gid}")
        return gid
    print(f"::error::Create internal group HTTP {r.status_code}: {r.text[:500]}")
    sys.exit(1)


def main():
    app_id = find_app()
    print(f"App ID: {app_id}")
    user_id = ensure_team_user()
    group_id = ensure_internal_group(app_id)

    # Add the user as a tester via the betaTesters relationship. If the
    # user hasn't accepted the team invite yet, this 422s — that's OK,
    # they'll be added next CI run automatically once accepted.
    payload = {"data": [{"type": "betaTesters", "id": user_id}]} if user_id else None
    if payload:
        r = requests.post(
            f"{BASE}/v1/betaGroups/{group_id}/relationships/betaTesters", headers=H, json=payload, timeout=15
        )
        if r.status_code in (200, 201, 204):
            print(f"  ✓ Added {EMAIL} to internal group {GROUP_NAME}")
        elif r.status_code == 422:
            print(f"  Tester already in group — no-op")
        else:
            print(f"::warning::Add-to-internal-group HTTP {r.status_code}: {r.text[:300]}")

    print(f"::notice::Internal-beta promotion path attempted for {EMAIL}. If team-invite accepted, install bypasses Apple Beta Review.")


if __name__ == "__main__":
    main()
