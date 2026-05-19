import { NextResponse } from "next/server";

import { auth } from "@/lib/auth";

/**
 * /api/customer/workspaces/init — first-login workspace bootstrapper.
 *
 * Issue #232: after magic-link sign-in the customer dashboard rendered
 * a "Pick a workspace" panel demanding the user paste a workspace UUID
 * by hand. No workspace existed yet, so the prompt was a dead end and
 * customers could not make a single API call after sign-up.
 *
 * This BFF proxy reads the authenticated NextAuth session, asks
 * identity-svc for the caller's existing workspaces, and creates a
 * default one if there are none. The browser only ever sees a
 * `{ workspace_id, name, created }` envelope — the user-id assertion
 * + service-token never touches the client bundle.
 *
 * Auth model (Phase 0):
 *   - The Next.js BFF holds the NextAuth cookie and trusts it.
 *   - It calls identity-svc with a shared `IOGRID_SERVICE_TOKEN` bearer
 *     + `X-Iogrid-User-Id` header. identity-svc's VerifyBearer accepts
 *     this combination as an authed context (see middleware.go).
 *   - When IOGRID_IDENTITY_SVC_URL or IOGRID_SERVICE_TOKEN is absent
 *     the route returns 503 — the caller (CustomerOverview) treats
 *     that as "fall back to the legacy paste-prompt".
 *
 * End state: once NextAuth→identity-svc token exchange ships, this
 * route can be deleted in favour of the browser calling
 * `/iogrid.identity.v1.WorkspaceService/{ListWorkspaces,CreateWorkspace}`
 * directly with the user's real JWT.
 */
export async function POST() {
  const session = await auth();
  if (!session?.user) {
    return NextResponse.json(
      { code: "unauthenticated", message: "sign in first" },
      { status: 401 },
    );
  }

  const userId = (session.user as { id?: string }).id ?? "";
  if (!userId) {
    return NextResponse.json(
      { code: "no_user_id", message: "session is missing user id" },
      { status: 500 },
    );
  }

  const upstream =
    process.env.IOGRID_IDENTITY_SVC_URL ??
    process.env.IOGRID_GATEWAY_BFF_URL ??
    "";
  const serviceToken = process.env.IOGRID_SERVICE_TOKEN ?? "";

  if (!upstream || !serviceToken) {
    // Caller renders the legacy paste-prompt as escape hatch.
    return NextResponse.json(
      {
        code: "auto_init_unavailable",
        message:
          "identity-svc service-token wiring not configured; falling back to manual paste",
      },
      { status: 503 },
    );
  }

  const baseHeaders: Record<string, string> = {
    "content-type": "application/json",
    authorization: `Bearer ${serviceToken}`,
    "x-iogrid-user-id": userId,
  };

  // 1) Look up existing workspaces. We prefer the JSON surface because
  //    it's stable across Connect-RPC re-codegens and the BFF doesn't
  //    bundle the protobuf runtime.
  const listURL = trimSlash(upstream) + "/v1/workspaces";
  let listRes: Response;
  try {
    listRes = await fetch(listURL, { method: "GET", headers: baseHeaders });
  } catch (err) {
    return NextResponse.json(
      {
        code: "identity_unreachable",
        message: `identity-svc unreachable: ${(err as Error).message}`,
      },
      { status: 502 },
    );
  }
  if (listRes.status === 401 || listRes.status === 403) {
    return NextResponse.json(
      { code: "unauthenticated", message: "identity-svc rejected the BFF token" },
      { status: 401 },
    );
  }
  if (!listRes.ok) {
    const t = await safeText(listRes);
    return NextResponse.json(
      { code: "list_failed", message: t || listRes.statusText },
      { status: 502 },
    );
  }
  const listBody = (await listRes.json()) as {
    workspaces?: Array<{ id: string; name: string }>;
  };
  const existing = listBody.workspaces ?? [];
  if (existing.length > 0) {
    const w = existing[0];
    return NextResponse.json({
      workspace_id: w.id,
      name: w.name,
      created: false,
    });
  }

  // 2) Zero workspaces — create the default. Display name is the
  //    local-part of the user's email (truncated to 32 chars), or
  //    "Workspace" when no email is bound.
  const defaultName = workspaceNameFromEmail(session.user.email);
  const createURL = trimSlash(upstream) + "/v1/workspaces";
  let createRes: Response;
  try {
    createRes = await fetch(createURL, {
      method: "POST",
      headers: baseHeaders,
      body: JSON.stringify({ name: defaultName, plan: "FREE" }),
    });
  } catch (err) {
    return NextResponse.json(
      {
        code: "identity_unreachable",
        message: `identity-svc unreachable on create: ${(err as Error).message}`,
      },
      { status: 502 },
    );
  }
  if (!createRes.ok) {
    const t = await safeText(createRes);
    return NextResponse.json(
      { code: "create_failed", message: t || createRes.statusText },
      { status: 502 },
    );
  }
  const created = (await createRes.json()) as { id: string; name: string };
  return NextResponse.json({
    workspace_id: created.id,
    name: created.name,
    created: true,
  });
}

function trimSlash(s: string): string {
  return s.replace(/\/$/, "");
}

async function safeText(res: Response): Promise<string> {
  try {
    return await res.text();
  } catch {
    return "";
  }
}

function workspaceNameFromEmail(email: string | null | undefined): string {
  const e = (email ?? "").trim();
  if (!e || !e.includes("@")) return "Workspace";
  const local = e.split("@")[0].slice(0, 32);
  return local || "Workspace";
}
