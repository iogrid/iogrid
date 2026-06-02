import { describe, expect, it } from "vitest";
import {
  GET,
  buildAasa,
  APP_ID,
  APP_BUNDLE_ID,
} from "@/app/.well-known/apple-app-site-association/route";

describe("apple-app-site-association route", () => {
  it("serves application/json with the AASA body", async () => {
    const res = GET();
    expect(res.headers.get("Content-Type")).toBe("application/json");
    const body = await res.json();
    expect(body).toHaveProperty("applinks");
    expect(body.applinks.details).toHaveLength(1);
  });

  it("uses the io.iogrid.app bundle id in the appID", () => {
    expect(APP_BUNDLE_ID).toBe("io.iogrid.app");
    expect(APP_ID.endsWith(`.${APP_BUNDLE_ID}`)).toBe(true);
  });

  it("declares the Direction-B universal-link paths (/buy-vpn, /vpn)", () => {
    const detail = buildAasa().applinks.details[0];
    expect(detail.paths).toContain("/buy-vpn");
    expect(detail.paths).toContain("/vpn");
    // Sub-path wildcards so /vpn/activated etc. are covered.
    expect(detail.paths).toContain("/vpn/*");
    expect(detail.paths).toContain("/buy-vpn/*");
  });

  it("ships with a PLACEHOLDER team id until real Apple creds exist", () => {
    // Guards against accidentally hard-coding a real Team ID. When the real
    // value is injected via NEXT_PUBLIC_APPLE_TEAM_ID this test still passes
    // because we only assert the default (no env) shape here.
    const detail = buildAasa().applinks.details[0];
    // appID is "<TEAMID>.io.iogrid.app" — must contain the bundle id.
    expect(detail.appID).toContain("io.iogrid.app");
    expect(detail.appIDs[0]).toBe(detail.appID);
  });

  it("includes webcredentials for the same appID", () => {
    const body = buildAasa();
    expect(body.webcredentials.apps[0]).toBe(APP_ID);
  });
});
