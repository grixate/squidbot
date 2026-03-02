import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import App from "./App";

const originalFetch = global.fetch;

function mockOnboardingFetch(password = "StrongPass23456789AB") {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    if (url === "/api/setup/state") {
      return new Response(
        JSON.stringify({
          setupComplete: false,
          requiresSetupToken: false,
          setupTokenExpiresAt: "",
          passwordMinLength: 12,
          providerCatalog: [
            {
              id: "openai",
              label: "OpenAI",
              requiresApiKey: true,
              requiresModel: false,
            },
          ],
        }),
        { status: 200 },
      );
    }
    if (url === "/api/setup/password/suggest") {
      return new Response(JSON.stringify({ password }), { status: 200 });
    }
    throw new Error(`unexpected fetch call for ${url}`);
  });
  global.fetch = fetchMock as typeof fetch;
  return fetchMock;
}

describe("App", () => {
  beforeEach(() => {
    window.history.replaceState({}, "", "/");
  });

  afterEach(() => {
    cleanup();
    global.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("renders the onboarding empty state", async () => {
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            setupComplete: false,
            requiresSetupToken: false,
            setupTokenExpiresAt: "",
            providerCatalog: [],
          }),
          { status: 200 },
        ),
      ) as typeof fetch;

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("No providers yet")).toBeInTheDocument();
    });
  });

  it("suggests a strong password and requires acknowledgement before the next step", async () => {
    const fetchMock = mockOnboardingFetch();

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("No providers yet")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: "Add provider" }));
    fireEvent.change(screen.getByLabelText("Provider"), { target: { value: "openai" } });
    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "sk-test" } });

    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => {
      expect(screen.getByText("Channels")).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("button", { name: "Next" }));

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Suggest strong password" })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: "Suggest strong password" }));

    await waitFor(() => {
      expect(screen.getByLabelText("Suggested password")).toHaveValue("StrongPass23456789AB");
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/setup/password/suggest",
      expect.objectContaining({ method: "POST" }),
    );
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();

    fireEvent.click(screen.getByLabelText("I saved this password"));

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Next" })).toBeEnabled();
    });
  });

  it("clears the generated-password acknowledgement requirement after manual edits", async () => {
    mockOnboardingFetch();

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("No providers yet")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: "Add provider" }));
    fireEvent.change(screen.getByLabelText("Provider"), { target: { value: "openai" } });
    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "sk-test" } });

    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => {
      expect(screen.getByText("Channels")).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Suggest strong password" })).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("button", { name: "Suggest strong password" }));

    await waitFor(() => {
      expect(screen.getByLabelText("I saved this password")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByLabelText("Management password"), { target: { value: "manual-password-123" } });

    await waitFor(() => {
      expect(screen.queryByLabelText("I saved this password")).not.toBeInTheDocument();
    });

    fireEvent.change(screen.getByLabelText("Confirm password"), { target: { value: "manual-password-123" } });

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Next" })).toBeEnabled();
    });
  });

  it("renders sign-in when setup is complete and the session is not authenticated", async () => {
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            setupComplete: true,
            requiresSetupToken: false,
            setupTokenExpiresAt: "",
            providerCatalog: [],
            current: { activeProviderId: "ollama", telegram: { enabled: false, tokenSet: false, allowFrom: [] } },
          }),
          { status: 200 },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            authenticated: false,
            setupComplete: true,
            activeProviderId: "ollama",
          }),
          { status: 200 },
        ),
      ) as typeof fetch;

    render(<App />);

    await waitFor(() => {
      expect(screen.getByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    });
  });

  it("renders the minimal home when the session is authenticated", async () => {
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            setupComplete: true,
            requiresSetupToken: false,
            setupTokenExpiresAt: "",
            providerCatalog: [],
            current: { activeProviderId: "ollama", telegram: { enabled: true, tokenSet: true, allowFrom: ["@alice"] } },
          }),
          { status: 200 },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            authenticated: true,
            setupComplete: true,
            activeProviderId: "ollama",
          }),
          { status: 200 },
        ),
      ) as typeof fetch;

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("Setup complete")).toBeInTheDocument();
    });
  });
});
