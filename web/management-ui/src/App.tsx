import { useEffect, useMemo, useState } from "react";
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  CollapsibleSection,
  Input,
  Label,
  Select,
  Separator,
  Shell,
} from "./components/ui";

type ProviderTemplate = {
  id: string;
  label: string;
  requiresApiKey: boolean;
  requiresModel: boolean;
  defaultApiBase?: string;
  defaultModel?: string;
};

type SetupState = {
  setupComplete: boolean;
  requiresSetupToken: boolean;
  setupTokenExpiresAt: string;
  passwordMinLength?: number;
  providerCatalog: ProviderTemplate[];
  current?: {
    providers?: SavedProviderSummary[];
    activeProviderId?: string;
    telegram?: { enabled: boolean; tokenSet: boolean; allowFrom: string[] };
  };
};

type SavedProviderSummary = {
  id: string;
  label: string;
  apiBase?: string;
  model?: string;
  hasApiKey: boolean;
};

type AuthSession = {
  authenticated: boolean;
  setupComplete: boolean;
  activeProviderId?: string;
};

type ProviderDraft = {
  uid: string;
  id: string;
  apiKey: string;
  apiBase: string;
  model: string;
  testing: boolean;
  testResult: "" | "ok" | "error";
  testMessage: string;
};

type TelegramDraft = {
  enabled: boolean;
  token: string;
  allowFrom: string[];
  pendingAllow: string;
};

type ScreenMode = "loading" | "onboarding" | "login" | "home";

const stepTitles = ["Provider", "Channels", "Security", "Review"] as const;

function parseError(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return "Request failed";
}

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    credentials: "same-origin",
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
  });
  if (!response.ok) {
    throw new Error((await response.text()) || `Request failed (${response.status})`);
  }
  return (await response.json()) as T;
}

function nextUID(): string {
  return Math.random().toString(36).slice(2, 10);
}

export default function App() {
  const [mode, setMode] = useState<ScreenMode>("loading");
  const [setupState, setSetupState] = useState<SetupState | null>(null);
  const [providers, setProviders] = useState<ProviderDraft[]>([]);
  const [activeProviderId, setActiveProviderId] = useState("");
  const [telegram, setTelegram] = useState<TelegramDraft | null>(null);
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [suggestedPassword, setSuggestedPassword] = useState("");
  const [generatedPasswordActive, setGeneratedPasswordActive] = useState(false);
  const [savedSuggestedPassword, setSavedSuggestedPassword] = useState(false);
  const [suggestingPassword, setSuggestingPassword] = useState(false);
  const [loginPassword, setLoginPassword] = useState("");
  const [currentStep, setCurrentStep] = useState(0);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [draftsBootstrapped, setDraftsBootstrapped] = useState(false);

  const setupToken = useMemo(() => new URLSearchParams(window.location.search).get("setup_token") ?? "", []);

  const providerCatalog = setupState?.providerCatalog ?? [];
  const passwordMinLength = setupState?.passwordMinLength ?? 12;

  useEffect(() => {
    void loadState();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (mode !== "onboarding" || !setupState || draftsBootstrapped) {
      return;
    }
    const seededProviders = (setupState.current?.providers ?? []).map((provider) => ({
      uid: nextUID(),
      id: provider.id,
      apiKey: "",
      apiBase: provider.apiBase ?? "",
      model: provider.model ?? "",
      testing: false,
      testResult: "",
      testMessage: "",
    }));
    if (seededProviders.length > 0) {
      setProviders(seededProviders);
    }
    if (setupState.current?.activeProviderId) {
      setActiveProviderId(setupState.current.activeProviderId);
    }
    if (setupState.current?.telegram && (setupState.current.telegram.enabled || setupState.current.telegram.tokenSet || setupState.current.telegram.allowFrom.length > 0)) {
      setTelegram({
        enabled: setupState.current.telegram.enabled,
        token: "",
        allowFrom: [...setupState.current.telegram.allowFrom],
        pendingAllow: "",
      });
    }
    setDraftsBootstrapped(true);
  }, [draftsBootstrapped, mode, setupState]);

  async function loadState() {
    setError("");
    setSuccess("");
    setMode("loading");
    try {
      const nextSetupState = await fetchJSON<SetupState>("/api/setup/state", {
        headers: undefined,
      });
      setSetupState(nextSetupState);
      if (!nextSetupState.setupComplete) {
        setDraftsBootstrapped(false);
        if (window.location.pathname !== "/") {
          window.history.replaceState({}, "", "/");
        }
        setMode("onboarding");
        return;
      }
      const session = await fetchJSON<AuthSession>("/api/auth/session", {
        headers: undefined,
      });
      if (session.authenticated) {
        if (window.location.pathname !== "/app") {
          window.history.replaceState({}, "", "/app");
        }
        setMode("home");
        return;
      }
      if (window.location.pathname !== "/") {
        window.history.replaceState({}, "", "/");
      }
      setMode("login");
    } catch (nextError) {
      setError(parseError(nextError));
      setMode("login");
    }
  }

  function availableProviderTemplates(currentID: string): ProviderTemplate[] {
    return providerCatalog.filter((entry) => {
      if (entry.id === currentID) {
        return true;
      }
      return !providers.some((draft) => draft.id === entry.id);
    });
  }

  function providerTemplateByID(providerID: string): ProviderTemplate | undefined {
    return providerCatalog.find((entry) => entry.id === providerID);
  }

  function updateProvider(uid: string, updater: (draft: ProviderDraft) => ProviderDraft) {
    setProviders((current) => current.map((draft) => (draft.uid === uid ? updater(draft) : draft)));
  }

  function addProvider() {
    setProviders((current) => [
      ...current,
      {
        uid: nextUID(),
        id: "",
        apiKey: "",
        apiBase: "",
        model: "",
        testing: false,
        testResult: "",
        testMessage: "",
      },
    ]);
  }

  function removeProvider(uid: string) {
    setProviders((current) => {
      const next = current.filter((draft) => draft.uid !== uid);
      if (!next.some((draft) => draft.id === activeProviderId)) {
        setActiveProviderId("");
      }
      return next;
    });
  }

  function setProviderSelection(uid: string, nextID: string) {
    const template = providerTemplateByID(nextID);
    updateProvider(uid, (draft) => ({
      ...draft,
      id: nextID,
      apiBase: draft.apiBase || template?.defaultApiBase || "",
      model: draft.model || template?.defaultModel || "",
      testResult: "",
      testMessage: "",
    }));
    if (!activeProviderId) {
      setActiveProviderId(nextID);
    }
  }

  function validationForProvider(draft: ProviderDraft): string[] {
    const template = providerTemplateByID(draft.id);
    const issues: string[] = [];
    if (!draft.id) {
      issues.push("Choose a provider.");
      return issues;
    }
    if (template?.requiresApiKey && !draft.apiKey.trim()) {
      issues.push("API key is required.");
    }
    if (template?.requiresModel && !draft.model.trim()) {
      issues.push("Model is required.");
    }
    return issues;
  }

  function providerStepReady(): boolean {
    if (providers.length === 0) {
      return false;
    }
    if (!activeProviderId || !providers.some((draft) => draft.id === activeProviderId)) {
      return false;
    }
    return providers.every((draft) => validationForProvider(draft).length === 0);
  }

  function securityStepReady(): boolean {
    if (password.trim().length < passwordMinLength || password !== confirmPassword) {
      return false;
    }
    if (generatedPasswordActive && !savedSuggestedPassword) {
      return false;
    }
    return true;
  }

  function currentStepReady(): boolean {
    if (currentStep === 0) {
      return providerStepReady();
    }
    if (currentStep === 2) {
      return securityStepReady();
    }
    return true;
  }

  async function testProvider(uid: string) {
    const draft = providers.find((item) => item.uid === uid);
    if (!draft) {
      return;
    }
    const issues = validationForProvider(draft);
    if (issues.length > 0) {
      updateProvider(uid, (current) => ({
        ...current,
        testResult: "error",
        testMessage: issues[0],
      }));
      return;
    }
    updateProvider(uid, (current) => ({
      ...current,
      testing: true,
      testResult: "",
      testMessage: "",
    }));
    try {
      const result = await fetchJSON<{ ok: boolean; error?: string }>("/api/setup/provider/test", {
        method: "POST",
        body: JSON.stringify({
          setupToken,
          provider: {
            id: draft.id,
            apiKey: draft.apiKey,
            apiBase: draft.apiBase,
            model: draft.model,
          },
        }),
      });
      updateProvider(uid, (current) => ({
        ...current,
        testing: false,
        testResult: result.ok ? "ok" : "error",
        testMessage: result.ok ? "Connection looks good." : result.error ?? "Provider test failed.",
      }));
    } catch (nextError) {
      updateProvider(uid, (current) => ({
        ...current,
        testing: false,
        testResult: "error",
        testMessage: parseError(nextError),
      }));
    }
  }

  function addTelegram() {
    setTelegram({
      enabled: true,
      token: "",
      allowFrom: [],
      pendingAllow: "",
    });
  }

  function addTelegramAllowValue() {
    if (!telegram) {
      return;
    }
    const nextValue = telegram.pendingAllow.trim();
    if (!nextValue) {
      return;
    }
    if (telegram.allowFrom.includes(nextValue)) {
      setTelegram({ ...telegram, pendingAllow: "" });
      return;
    }
    setTelegram({
      ...telegram,
      pendingAllow: "",
      allowFrom: [...telegram.allowFrom, nextValue],
    });
  }

  function removeTelegramAllowValue(value: string) {
    if (!telegram) {
      return;
    }
    setTelegram({
      ...telegram,
      allowFrom: telegram.allowFrom.filter((entry) => entry !== value),
    });
  }

  function clearSuggestedPasswordState() {
    setSuggestedPassword("");
    setGeneratedPasswordActive(false);
    setSavedSuggestedPassword(false);
  }

  async function suggestPassword() {
    setSuggestingPassword(true);
    setError("");
    setSuccess("");
    try {
      const result = await fetchJSON<{ password: string }>("/api/setup/password/suggest", {
        method: "POST",
        body: JSON.stringify({ setupToken }),
      });
      setPassword(result.password);
      setConfirmPassword(result.password);
      setSuggestedPassword(result.password);
      setGeneratedPasswordActive(true);
      setSavedSuggestedPassword(false);
    } catch (nextError) {
      setError(parseError(nextError));
    } finally {
      setSuggestingPassword(false);
    }
  }

  async function completeSetup() {
    if (!providerStepReady() || !securityStepReady()) {
      return;
    }
    setSubmitting(true);
    setError("");
    setSuccess("");
    try {
      await fetchJSON<{ ok: boolean }>("/api/setup/complete", {
        method: "POST",
        body: JSON.stringify({
          setupToken,
          providers: providers.map((draft) => ({
            id: draft.id,
            apiKey: draft.apiKey.trim(),
            apiBase: draft.apiBase.trim(),
            model: draft.model.trim(),
          })),
          activeProviderId,
          telegram: telegram
            ? {
                enabled: telegram.enabled,
                token: telegram.token.trim(),
                allowFrom: telegram.allowFrom,
              }
            : undefined,
          password,
        }),
      });
      setSuccess("Setup completed.");
      setPassword("");
      setConfirmPassword("");
      clearSuggestedPasswordState();
      setCurrentStep(0);
      window.history.replaceState({}, "", "/");
      await loadState();
    } catch (nextError) {
      setError(parseError(nextError));
    } finally {
      setSubmitting(false);
    }
  }

  async function login() {
    setSubmitting(true);
    setError("");
    setSuccess("");
    try {
      await fetchJSON<{ ok: boolean }>("/api/auth/login", {
        method: "POST",
        body: JSON.stringify({ password: loginPassword }),
      });
      setLoginPassword("");
      await loadState();
    } catch (nextError) {
      setError(parseError(nextError));
    } finally {
      setSubmitting(false);
    }
  }

  async function logout() {
    setSubmitting(true);
    setError("");
    setSuccess("");
    try {
      await fetchJSON<{ ok: boolean }>("/api/auth/logout", {
        method: "POST",
        body: JSON.stringify({}),
      });
      await loadState();
    } catch (nextError) {
      setError(parseError(nextError));
    } finally {
      setSubmitting(false);
    }
  }

  const progressDots = (
    <div className="flex gap-2" aria-hidden="true">
      {stepTitles.map((_, index) => (
        <span
          key={index}
          className={`h-2 w-2 rounded-full ${index === currentStep ? "bg-slate-900" : index < currentStep ? "bg-slate-300" : "bg-slate-200"}`}
        />
      ))}
    </div>
  );

  const pageHeading = mode === "home" ? "Mission Control" : "Mission Control setup";
  const pageDescription =
    mode === "home"
      ? "Setup is finished. This lightweight home screen keeps the next action obvious."
      : mode === "login"
        ? "Your local setup is ready. Sign in to confirm everything is working."
        : "A calmer first-run flow: only the essentials now, with optional details added only when you ask for them.";

  const currentStepTone =
    currentStep === 1 ? "Optional step" : currentStep === 3 ? "Final check" : "Required step";

  return (
    <Shell>
      <header className="mx-auto flex w-full max-w-3xl flex-col gap-3 px-2 pt-2">
        <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Squidbot</p>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div className="space-y-2">
            <h1 className="text-3xl font-semibold tracking-tight text-slate-950">{pageHeading}</h1>
            <p className="max-w-2xl text-sm leading-6 text-slate-600">{pageDescription}</p>
          </div>
          {mode === "onboarding" && (
            <div className="flex items-center gap-3 rounded-full border border-slate-200 bg-white px-4 py-2 text-sm text-slate-600 shadow-[0_1px_2px_rgba(15,23,42,0.03)]">
              <span>{`Step ${currentStep + 1} of 4`}</span>
              {progressDots}
            </div>
          )}
        </div>
      </header>

      {error ? <Alert tone="error">{error}</Alert> : null}
      {success ? <Alert tone="success">{success}</Alert> : null}

      {mode === "loading" ? (
        <Card className="mx-auto w-full max-w-2xl">
          <CardContent className="py-8">
            <p className="text-sm text-slate-600">Loading setup state…</p>
          </CardContent>
        </Card>
      ) : null}

      {mode === "onboarding" ? (
        <Card className="mx-auto w-full max-w-2xl">
          <CardHeader>
            <div className="flex flex-wrap items-center gap-2">
              <Badge>{currentStepTone}</Badge>
            </div>
            <CardTitle>{stepTitles[currentStep]}</CardTitle>
            <CardDescription>
              {currentStep === 0 && "Start with the providers you actually want to keep. Nothing is shown until you add it."}
              {currentStep === 1 && "Channels are optional. Add Telegram only if you want it now."}
              {currentStep === 2 && "Finish setup with a management password. This is the only required security setting in v1."}
              {currentStep === 3 && "Review the essentials before writing the config."}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-6">
            {currentStep === 0 ? (
              <section className="space-y-5">
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <p className="text-sm font-medium text-slate-800">Saved provider list</p>
                    <p className="text-sm text-slate-500">Build the list first, then mark one entry as active.</p>
                  </div>
                  {providers.length > 0 ? (
                    <Button type="button" variant="secondary" onClick={addProvider}>
                      Add provider
                    </Button>
                  ) : null}
                </div>
                {providers.length === 0 ? (
                  <div className="rounded-3xl border border-dashed border-slate-300 bg-slate-50 px-5 py-8 text-center">
                    <p className="text-sm font-medium text-slate-700">No providers yet</p>
                    <p className="mt-2 text-sm text-slate-500">Add only the providers you want saved during setup.</p>
                    <Button className="mt-4" type="button" onClick={addProvider}>
                      Add provider
                    </Button>
                  </div>
                ) : (
                  <div className="space-y-4">
                    {providers.map((draft, index) => {
                      const template = providerTemplateByID(draft.id);
                      const issues = validationForProvider(draft);
                      return (
                        <div key={draft.uid} className="rounded-3xl border border-slate-200 bg-slate-50/70 p-5">
                          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                            <div className="space-y-1">
                              <p className="text-sm font-semibold text-slate-800">{template?.label || `Provider ${index + 1}`}</p>
                              <p className="text-xs text-slate-500">Exactly one provider must be marked active.</p>
                            </div>
                            <div className="flex flex-wrap gap-2">
                              {draft.id && activeProviderId === draft.id ? <Badge>Active provider</Badge> : null}
                              <Button type="button" variant="ghost" onClick={() => removeProvider(draft.uid)}>
                                Remove
                              </Button>
                            </div>
                          </div>
                          <div className="mt-5 grid gap-4">
                            <div className="grid gap-2">
                              <Label htmlFor={`provider-id-${draft.uid}`}>Provider</Label>
                              <Select
                                id={`provider-id-${draft.uid}`}
                                value={draft.id}
                                onChange={(event) => setProviderSelection(draft.uid, event.target.value)}
                              >
                                <option value="">Choose a provider</option>
                                {availableProviderTemplates(draft.id).map((entry) => (
                                  <option key={entry.id} value={entry.id}>
                                    {entry.label}
                                  </option>
                                ))}
                              </Select>
                            </div>
                            {template ? (
                              <>
                                <div className="grid gap-2">
                                  <Label htmlFor={`provider-key-${draft.uid}`}>
                                    {template.requiresApiKey ? "API key" : "API key (optional)"}
                                  </Label>
                                  <Input
                                    id={`provider-key-${draft.uid}`}
                                    value={draft.apiKey}
                                    type="password"
                                    autoComplete="off"
                                    onChange={(event) =>
                                      updateProvider(draft.uid, (current) => ({ ...current, apiKey: event.target.value, testResult: "", testMessage: "" }))
                                    }
                                  />
                                </div>
                                <div className="grid gap-2">
                                  <Label className="inline-flex items-center gap-2 rounded-2xl border border-slate-200 bg-white px-4 py-3">
                                    <input
                                      checked={activeProviderId === draft.id}
                                      className="h-4 w-4"
                                      name="active-provider"
                                      type="radio"
                                      value={draft.id}
                                      onChange={() => setActiveProviderId(draft.id)}
                                    />
                                    Mark as active
                                  </Label>
                                </div>
                                <CollapsibleSection title="Advanced fields">
                                  <div className="grid gap-4">
                                    <div className="grid gap-2">
                                      <Label htmlFor={`provider-base-${draft.uid}`}>API base</Label>
                                      <Input
                                        id={`provider-base-${draft.uid}`}
                                        value={draft.apiBase}
                                        autoComplete="off"
                                        onChange={(event) =>
                                          updateProvider(draft.uid, (current) => ({ ...current, apiBase: event.target.value, testResult: "", testMessage: "" }))
                                        }
                                      />
                                    </div>
                                    <div className="grid gap-2">
                                      <Label htmlFor={`provider-model-${draft.uid}`}>
                                        {template.requiresModel ? "Model" : "Model (optional)"}
                                      </Label>
                                      <Input
                                        id={`provider-model-${draft.uid}`}
                                        value={draft.model}
                                        autoComplete="off"
                                        onChange={(event) =>
                                          updateProvider(draft.uid, (current) => ({ ...current, model: event.target.value, testResult: "", testMessage: "" }))
                                        }
                                      />
                                    </div>
                                  </div>
                                </CollapsibleSection>
                                <div className="flex flex-wrap items-center gap-3">
                                  <Button type="button" variant="secondary" disabled={draft.testing} onClick={() => void testProvider(draft.uid)}>
                                    {draft.testing ? "Testing…" : "Test connection"}
                                  </Button>
                                  {draft.testMessage ? (
                                    <span className={`text-sm ${draft.testResult === "ok" ? "text-emerald-700" : "text-slate-600"}`}>
                                      {draft.testMessage}
                                    </span>
                                  ) : null}
                                </div>
                                {issues.length > 0 ? <Alert tone="error">{issues[0]}</Alert> : null}
                              </>
                            ) : null}
                          </div>
                        </div>
                      );
                    })}
                  </div>
                )}
              </section>
            ) : null}

            {currentStep === 1 ? (
              <section className="space-y-5">
                {!telegram ? (
                  <div className="rounded-3xl border border-dashed border-slate-300 bg-slate-50 px-5 py-8 text-center">
                    <p className="text-sm font-medium text-slate-700">No channels configured</p>
                    <p className="mt-2 text-sm text-slate-500">Add Telegram only if you want messages routed in from it during setup.</p>
                    <Button className="mt-4" type="button" onClick={addTelegram}>
                      Add Telegram
                    </Button>
                  </div>
                ) : (
                  <div className="rounded-3xl border border-slate-200 bg-slate-50/70 p-5">
                    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                      <div className="space-y-1">
                        <p className="text-sm font-semibold text-slate-800">Telegram</p>
                        <p className="text-xs text-slate-500">Still optional. Remove it and setup completes without any channels.</p>
                      </div>
                      <Button type="button" variant="ghost" onClick={() => setTelegram(null)}>
                        Remove
                      </Button>
                    </div>
                    <div className="mt-5 grid gap-4">
                      <Label className="inline-flex items-center gap-2">
                        <input
                          checked={telegram.enabled}
                          className="h-4 w-4"
                          type="checkbox"
                          onChange={(event) => setTelegram({ ...telegram, enabled: event.target.checked })}
                        />
                        Enable Telegram
                      </Label>
                      <div className="grid gap-2">
                        <Label htmlFor="telegram-token">Bot token</Label>
                        <Input
                          id="telegram-token"
                          type="password"
                          autoComplete="off"
                          value={telegram.token}
                          onChange={(event) => setTelegram({ ...telegram, token: event.target.value })}
                        />
                      </div>
                      <div className="grid gap-3">
                        <Label htmlFor="telegram-allow">Allowed senders</Label>
                        <div className="flex flex-col gap-3 sm:flex-row">
                          <Input
                            id="telegram-allow"
                            placeholder="@alice or 123456"
                            value={telegram.pendingAllow}
                            onChange={(event) => setTelegram({ ...telegram, pendingAllow: event.target.value })}
                            onKeyDown={(event) => {
                              if (event.key === "Enter") {
                                event.preventDefault();
                                addTelegramAllowValue();
                              }
                            }}
                          />
                          <Button type="button" variant="secondary" onClick={addTelegramAllowValue}>
                            Add
                          </Button>
                        </div>
                        {telegram.allowFrom.length > 0 ? (
                          <div className="flex flex-wrap gap-2">
                            {telegram.allowFrom.map((entry) => (
                              <Badge key={entry}>
                                {entry}
                                <button
                                  aria-label={`Remove ${entry}`}
                                  className="rounded-full text-slate-500 transition hover:text-slate-800"
                                  type="button"
                                  onClick={() => removeTelegramAllowValue(entry)}
                                >
                                  ×
                                </button>
                              </Badge>
                            ))}
                          </div>
                        ) : (
                          <p className="text-sm text-slate-500">No sender restrictions yet.</p>
                        )}
                      </div>
                    </div>
                  </div>
                )}
              </section>
            ) : null}

            {currentStep === 2 ? (
              <section className="grid gap-4">
                <div className="grid gap-2">
                  <Label htmlFor="password">Management password</Label>
                  <Input
                    id="password"
                    type="password"
                    value={password}
                    onChange={(event) => {
                      if (generatedPasswordActive) {
                        clearSuggestedPasswordState();
                      }
                      setPassword(event.target.value);
                    }}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="confirm-password">Confirm password</Label>
                  <Input
                    id="confirm-password"
                    type="password"
                    value={confirmPassword}
                    onChange={(event) => {
                      if (generatedPasswordActive) {
                        clearSuggestedPasswordState();
                      }
                      setConfirmPassword(event.target.value);
                    }}
                  />
                </div>
                <div className="flex flex-wrap items-center gap-3">
                  <Button type="button" variant="secondary" disabled={suggestingPassword} onClick={() => void suggestPassword()}>
                    {suggestingPassword ? "Generating…" : "Suggest strong password"}
                  </Button>
                  <span className="text-sm text-slate-500">Manual entry still works if you prefer to choose your own.</span>
                </div>
                {generatedPasswordActive ? (
                  <div className="grid gap-3 rounded-3xl border border-slate-200 bg-slate-50/70 p-4">
                    <div className="grid gap-2">
                      <Label htmlFor="suggested-password">Suggested password</Label>
                      <Input id="suggested-password" readOnly value={suggestedPassword} />
                    </div>
                    <Label className="inline-flex items-center gap-2 text-sm text-slate-700">
                      <input
                        checked={savedSuggestedPassword}
                        className="h-4 w-4"
                        type="checkbox"
                        onChange={(event) => setSavedSuggestedPassword(event.target.checked)}
                      />
                      I saved this password
                    </Label>
                  </div>
                ) : null}
                <Alert tone={securityStepReady() ? "success" : "neutral"}>
                  Password must be at least {passwordMinLength} characters and the confirmation must match.
                  {generatedPasswordActive ? " If you keep the suggestion, confirm that you saved it before finishing setup." : ""}
                </Alert>
              </section>
            ) : null}

            {currentStep === 3 ? (
              <section className="space-y-5">
                <div className="rounded-3xl border border-slate-200 bg-slate-50/70 p-5">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <p className="text-sm font-semibold text-slate-800">Providers</p>
                      <p className="text-sm text-slate-500">{providers.length} configured</p>
                    </div>
                    <Button type="button" variant="ghost" onClick={() => setCurrentStep(0)}>
                      Edit
                    </Button>
                  </div>
                  <div className="mt-4 space-y-3">
                    {providers.map((draft) => (
                      <div key={draft.uid} className="flex flex-wrap items-center gap-2 text-sm text-slate-600">
                        <Badge>{providerTemplateByID(draft.id)?.label ?? "Unselected"}</Badge>
                        {draft.id === activeProviderId ? <Badge className="bg-slate-900 text-white">Active</Badge> : null}
                        {validationForProvider(draft).length > 0 ? <span>{validationForProvider(draft)[0]}</span> : null}
                      </div>
                    ))}
                  </div>
                </div>
                <div className="rounded-3xl border border-slate-200 bg-slate-50/70 p-5">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <p className="text-sm font-semibold text-slate-800">Channels</p>
                      <p className="text-sm text-slate-500">{telegram ? "Telegram will be saved." : "No channels selected."}</p>
                    </div>
                    <Button type="button" variant="ghost" onClick={() => setCurrentStep(1)}>
                      Edit
                    </Button>
                  </div>
                  {telegram ? (
                    <p className="mt-4 text-sm text-slate-600">
                      Telegram is {telegram.enabled ? "enabled" : "disabled"} with {telegram.allowFrom.length} allow-list entries.
                    </p>
                  ) : null}
                </div>
                <div className="rounded-3xl border border-slate-200 bg-slate-50/70 p-5">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <p className="text-sm font-semibold text-slate-800">Security</p>
                      <p className="text-sm text-slate-500">{securityStepReady() ? "Password is ready." : "Password still needs attention."}</p>
                    </div>
                    <Button type="button" variant="ghost" onClick={() => setCurrentStep(2)}>
                      Edit
                    </Button>
                  </div>
                </div>
                {!providerStepReady() ? <Alert tone="error">Provider setup is incomplete.</Alert> : null}
                {!securityStepReady() ? <Alert tone="error">Password setup is incomplete.</Alert> : null}
              </section>
            ) : null}

            <Separator />
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="text-sm text-slate-500">
                {currentStep === 3 ? "Optional tests do not block completion." : "Only the minimum required information blocks the next step."}
              </div>
              <div className="flex flex-wrap gap-3">
                <Button type="button" variant="ghost" disabled={currentStep === 0} onClick={() => setCurrentStep((step) => Math.max(0, step - 1))}>
                  Back
                </Button>
                {currentStep < 3 ? (
                  <Button type="button" disabled={!currentStepReady()} onClick={() => setCurrentStep((step) => Math.min(3, step + 1))}>
                    Next
                  </Button>
                ) : (
                  <Button type="button" disabled={submitting || !providerStepReady() || !securityStepReady()} onClick={() => void completeSetup()}>
                    {submitting ? "Saving…" : "Complete setup"}
                  </Button>
                )}
              </div>
            </div>
          </CardContent>
        </Card>
      ) : null}

      {mode === "login" ? (
        <Card className="mx-auto w-full max-w-xl">
          <CardHeader>
            <CardTitle>Sign in</CardTitle>
            <CardDescription>Setup is complete. Sign in to view the minimal local home screen.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-2">
              <Label htmlFor="login-password">Password</Label>
              <Input
                id="login-password"
                type="password"
                value={loginPassword}
                onChange={(event) => setLoginPassword(event.target.value)}
              />
            </div>
            <Button type="button" disabled={submitting || !loginPassword.trim()} onClick={() => void login()}>
              {submitting ? "Signing in…" : "Sign in"}
            </Button>
          </CardContent>
        </Card>
      ) : null}

      {mode === "home" ? (
        <Card className="mx-auto w-full max-w-2xl">
          <CardHeader>
            <CardTitle>Setup complete</CardTitle>
            <CardDescription>The minimal local home screen keeps the next action obvious while the full configuration UI is still pending.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-5">
            <div className="grid gap-3 rounded-3xl border border-slate-200 bg-slate-50/70 p-5 text-sm text-slate-600">
              <div className="flex flex-wrap items-center gap-2">
                <span>Active provider:</span>
                <Badge>{setupState?.current?.activeProviderId || "Unknown"}</Badge>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <span>Telegram:</span>
                <Badge>{setupState?.current?.telegram?.enabled ? "Configured" : "Not configured"}</Badge>
              </div>
            </div>
            <div className="space-y-2 text-sm text-slate-600">
              <p>Next actions:</p>
              <ul className="list-disc space-y-1 pl-5">
                <li>Run <code className="rounded bg-slate-100 px-1.5 py-0.5">squidbot agent -m "hello"</code> to test the assistant.</li>
                <li>Use <code className="rounded bg-slate-100 px-1.5 py-0.5">squidbot gateway</code> when you are ready for channel traffic.</li>
                <li>Return here later as the fuller management surface expands.</li>
              </ul>
            </div>
            <Button type="button" variant="secondary" disabled={submitting} onClick={() => void logout()}>
              {submitting ? "Signing out…" : "Log out"}
            </Button>
          </CardContent>
        </Card>
      ) : null}
    </Shell>
  );
}
