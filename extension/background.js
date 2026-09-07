/* ============================================================
   Clipboard Vault — Background Service Worker
   Cross-browser WebExtension standard (Chrome & Firefox)
   Handles API communication, auto-detection, auto-saving, autofill,
   API key management, and extension auto-updating
   ============================================================ */

const DEFAULT_SERVER_URL = "https://clip.codesky.tech";

chrome.runtime.onInstalled.addListener(() => {
  chrome.storage.local.get(["serverUrl", "autoFill", "autoSave"], (res) => {
    const updates = {};
    if (!res.serverUrl || res.serverUrl === "http://localhost:8080") {
      updates.serverUrl = DEFAULT_SERVER_URL;
    }
    if (res.autoFill === undefined) updates.autoFill = true;
    if (res.autoSave === undefined) updates.autoSave = true;
    chrome.storage.local.set(updates, () => {
      checkForUpdates();
    });
  });

  // Setup periodic update check alarm (every 60 minutes)
  try {
    chrome.alarms.create("check_update", { periodInMinutes: 60 });
  } catch {
    // alarms may not be available in temporary contexts
  }
});

chrome.runtime.onStartup?.addListener(() => {
  checkForUpdates();
});

chrome.alarms?.onAlarm?.addListener((alarm) => {
  if (alarm.name === "check_update") {
    checkForUpdates();
  }
});

// ───────────────────────── Auto-Update Engine ─────────────────────────

async function checkForUpdates() {
  try {
    const store = await chrome.storage.local.get(["serverUrl"]);
    const serverUrl = (store.serverUrl || DEFAULT_SERVER_URL).replace(/\/+$/, "");
    const res = await fetch(`${serverUrl}/api/extension/version`, { cache: "no-cache" });
    if (!res.ok) return { updateAvailable: false };

    const data = await res.json();
    const currentVersion = chrome.runtime.getManifest().version;
    const isNewer = compareVersions(data.version, currentVersion) > 0;

    if (isNewer) {
      const updateInfo = {
        available: true,
        version: data.version,
        downloadUrl: data.download_url,
        releaseUrl: data.release_url,
      };
      await chrome.storage.local.set({ updateInfo });
      try {
        chrome.action.setBadgeText({ text: "NEW" });
        chrome.action.setBadgeBackgroundColor({ color: "#2563eb" });
      } catch {}
      return { updateAvailable: true, updateInfo };
    } else {
      await chrome.storage.local.remove(["updateInfo"]);
      try {
        chrome.action.setBadgeText({ text: "" });
      } catch {}
      return { updateAvailable: false };
    }
  } catch (err) {
    return { updateAvailable: false, error: err.message };
  }
}

function compareVersions(v1, v2) {
  if (!v1 || !v2) return 0;
  const p1 = v1.split(".").map(Number);
  const p2 = v2.split(".").map(Number);
  for (let i = 0; i < Math.max(p1.length, p2.length); i++) {
    const num1 = p1[i] || 0;
    const num2 = p2[i] || 0;
    if (num1 > num2) return 1;
    if (num1 < num2) return -1;
  }
  return 0;
}

// ───────────────────────── Helpers ─────────────────────────

async function getAuthToken(serverUrl) {
  const store = await chrome.storage.local.get(["token"]);
  if (store.token) return store.token;

  try {
    const res = await fetch(`${serverUrl}/api/auth/me`, { credentials: "include" });
    if (res.ok) {
      const data = await res.json();
      if (data.token) {
        await chrome.storage.local.set({ token: data.token });
        return data.token;
      }
    }
  } catch {}
  return null;
}

function cleanHost(hostname) {
  return (hostname || "").toLowerCase().replace(/^www\./, "").trim();
}

// ───────────────────────── Message Router ─────────────────────────

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  // 1. Check for updates manual trigger
  if (message.type === "CHECK_FOR_UPDATES") {
    checkForUpdates().then((res) => sendResponse(res));
    return true;
  }

  // 2. Query password credentials for automatic autofill
  if (message.type === "QUERY_CREDENTIALS") {
    (async () => {
      try {
        const config = await chrome.storage.local.get(["serverUrl", "autoFill"]);
        if (config.autoFill === false) {
          sendResponse({ authenticated: false, autoFillDisabled: true, secrets: [] });
          return;
        }

        const serverUrl = (config.serverUrl || DEFAULT_SERVER_URL).replace(/\/+$/, "");
        const token = await getAuthToken(serverUrl);
        if (!token) {
          sendResponse({ authenticated: false, secrets: [] });
          return;
        }

        const host = cleanHost(message.hostname);
        if (!host) {
          sendResponse({ authenticated: true, secrets: [] });
          return;
        }

        let res = await fetch(`${serverUrl}/api/secrets/lookup?url=${encodeURIComponent(host)}`, {
          headers: { Authorization: `Bearer ${token}` },
        });

        let data = res.ok ? await res.json() : { secrets: [] };
        let secrets = data.secrets || [];

        // Subdomain fallback (e.g. login.github.com -> github.com)
        if (secrets.length === 0) {
          const parts = host.split(".");
          if (parts.length > 2) {
            const rootDomain = parts.slice(-2).join(".");
            const rootRes = await fetch(`${serverUrl}/api/secrets/lookup?url=${encodeURIComponent(rootDomain)}`, {
              headers: { Authorization: `Bearer ${token}` },
            });
            if (rootRes.ok) {
              const rootData = await rootRes.json();
              secrets = rootData.secrets || [];
            }
          }
        }

        sendResponse({ authenticated: true, secrets });
      } catch (err) {
        sendResponse({ authenticated: false, error: err.message, secrets: [] });
      }
    })();
    return true;
  }

  // 3. Query API keys for developers / token fields
  if (message.type === "QUERY_API_KEYS") {
    (async () => {
      try {
        const config = await chrome.storage.local.get(["serverUrl"]);
        const serverUrl = (config.serverUrl || DEFAULT_SERVER_URL).replace(/\/+$/, "");
        const token = await getAuthToken(serverUrl);
        if (!token) {
          sendResponse({ authenticated: false, apiKeys: [] });
          return;
        }

        const host = cleanHost(message.hostname || "");
        // Query API keys specific to platform or global API keys
        const url = host
          ? `${serverUrl}/api/secrets/lookup?url=${encodeURIComponent(host)}&kind=api_key`
          : `${serverUrl}/api/secrets/lookup?kind=api_key`;

        const res = await fetch(url, {
          headers: { Authorization: `Bearer ${token}` },
        });

        if (!res.ok) {
          sendResponse({ authenticated: true, apiKeys: [] });
          return;
        }

        const data = await res.json();
        sendResponse({ authenticated: true, apiKeys: data.secrets || [] });
      } catch (err) {
        sendResponse({ authenticated: false, error: err.message, apiKeys: [] });
      }
    })();
    return true;
  }

  // 4. Automatic saving and updating of credentials / API keys
  if (message.type === "AUTO_SAVE_CREDENTIAL") {
    (async () => {
      try {
        const config = await chrome.storage.local.get(["serverUrl", "autoSave"]);
        const serverUrl = (config.serverUrl || DEFAULT_SERVER_URL).replace(/\/+$/, "");
        const token = await getAuthToken(serverUrl);

        const username = (message.username || "").trim();
        const value = message.password || message.value || "";
        const url = message.url || "";
        const hostname = cleanHost(message.hostname || "");
        const kind = message.kind || "password";
        const title = message.title || (hostname ? `${hostname.charAt(0).toUpperCase() + hostname.slice(1)} ${kind === "api_key" ? "API Key" : "Account"}` : "Saved Secret");

        if (!value) {
          sendResponse({ success: false, reason: "empty_value" });
          return;
        }

        // Cache in pendingSave as fallback
        await chrome.storage.local.set({
          pendingSave: {
            url,
            username,
            password: value,
            kind,
            title,
            timestamp: Date.now(),
          },
        });

        if (!token) {
          sendResponse({ success: false, reason: "unauthenticated" });
          return;
        }

        if (config.autoSave === false) {
          sendResponse({ success: false, reason: "autosave_disabled", pending: true });
          return;
        }

        // Lookup existing secrets for this domain / platform
        const lookupRes = await fetch(`${serverUrl}/api/secrets/lookup?url=${encodeURIComponent(hostname)}`, {
          headers: { Authorization: `Bearer ${token}` },
        });

        let existing = null;
        if (lookupRes.ok) {
          const lookupData = await lookupRes.json();
          const list = lookupData.secrets || [];

          if (kind === "api_key") {
            // Check if same API key value or same title
            existing = list.find((s) => s.value === value || s.title.toLowerCase() === title.toLowerCase());
          } else {
            if (username) {
              existing = list.find((s) => (s.username || "").toLowerCase() === username.toLowerCase());
            }
            if (!existing && list.length === 1 && !username) {
              existing = list[0];
            }
          }
        }

        if (existing) {
          if (existing.value === value) {
            sendResponse({ success: true, status: "unchanged", title: existing.title });
            return;
          }

          // Update existing credential / API key
          const updateRes = await fetch(`${serverUrl}/api/secrets/${existing.id}`, {
            method: "PUT",
            headers: {
              "Content-Type": "application/json",
              Authorization: `Bearer ${token}`,
            },
            body: JSON.stringify({
              title: existing.title,
              kind: existing.kind || kind,
              username: username || existing.username,
              url: existing.url || url,
              value: value,
              notes: existing.notes || message.notes || "",
            }),
          });

          if (updateRes.ok) {
            await chrome.storage.local.remove(["pendingSave"]);
            sendResponse({ success: true, status: "updated", title: existing.title, username });
          } else {
            sendResponse({ success: false, reason: "update_failed" });
          }
          return;
        }

        // Create new credential / API key
        const createRes = await fetch(`${serverUrl}/api/secrets`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${token}`,
          },
          body: JSON.stringify({
            title,
            kind,
            username,
            url,
            value,
            notes: message.notes || "",
          }),
        });

        if (createRes.ok) {
          await chrome.storage.local.remove(["pendingSave"]);
          sendResponse({ success: true, status: "created", title, username });
        } else {
          sendResponse({ success: false, reason: "create_failed" });
        }
      } catch (err) {
        sendResponse({ success: false, error: err.message });
      }
    })();
    return true;
  }
});
