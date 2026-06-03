import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { SignInPanel } from "@/app/account/sign-in";
import { googleSignInEnabled } from "@/lib/google-oauth";

const noop = async () => {};

describe("googleSignInEnabled", () => {
  it("is false when the client id is unset", () => {
    expect(googleSignInEnabled(undefined)).toBe(false);
  });

  it("is false for an empty / whitespace client id", () => {
    expect(googleSignInEnabled("")).toBe(false);
    expect(googleSignInEnabled("   ")).toBe(false);
  });

  it("is false for the phase0 placeholder seed", () => {
    expect(googleSignInEnabled("phase0-placeholder")).toBe(false);
    expect(googleSignInEnabled("  phase0-placeholder  ")).toBe(false);
    expect(googleSignInEnabled("PHASE0-PLACEHOLDER")).toBe(false);
  });

  it("is true for a real-looking Google client id", () => {
    expect(
      googleSignInEnabled("123456789-abcdef.apps.googleusercontent.com"),
    ).toBe(true);
  });
});

describe("SignInPanel", () => {
  it("hides the Google button when Google is not configured", () => {
    render(
      <SignInPanel
        signInWithGoogle={noop}
        signInWithEmail={noop}
        googleEnabled={false}
      />,
    );
    expect(screen.queryByText("Continue with Google")).not.toBeInTheDocument();
    // magic-link path is always present and working
    expect(screen.getByText("Send magic link")).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
  });

  it("shows the Google button when Google is configured", () => {
    render(
      <SignInPanel
        signInWithGoogle={noop}
        signInWithEmail={noop}
        googleEnabled={true}
      />,
    );
    expect(screen.getByText("Continue with Google")).toBeInTheDocument();
    expect(screen.getByText("Send magic link")).toBeInTheDocument();
  });

  it("defaults to showing the Google button when the flag is omitted", () => {
    render(<SignInPanel signInWithGoogle={noop} signInWithEmail={noop} />);
    expect(screen.getByText("Continue with Google")).toBeInTheDocument();
  });
});
