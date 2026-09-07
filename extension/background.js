/* ============================================================
   Clipboard Vault — Background Service Worker
   Cross-browser WebExtension standard (Chrome & Firefox)
   Handles API communication, auto-detection, auto-saving, and autofill
   ============================================================ */

chrome.runtime.onInstalled.addListener(() => {
  chrome.storage.local.get(["serverUrl", "autoFill", "autoSave"], (res) => {
    const updates = {};
    if (!res.serverUrl || res.serverUrl === "https://clip.codesky.tech") {
      updates.serverUrl = "https://clip.codesky.tech";
    }
    if (res.autoFill === undefined) updates.autoFill = true;
    if (res.autoSave === undefined) updates.autoSave = true;
    chrome.storage.local.set(updates);
  });
});

// Helper: Ensure token is fresh, attempt cookie sync if needed
async function getAuthToken(serverUrl) {
  const store = await chrome.storage.local.get(["token"]);
  if (store.token) return store.token;

  // Try auto cookie sync if user signed in via web dashboard
  try {
    const res = await fetch(`${serverUrl}/api/auth/me`, { credentials: "include" });
    if (res.ok) {
      const data = await res.json();
      if (data.token) {
        await chrome.storage.local.set({ token: data.token });
        return data.token;
      }
    }
  } catch {
    // server unreachable or no session
  }
  return null;
}

// Clean hostname helper
function cleanHost(hostname) {
  return (hostname || "").toLowerCase().replace(/^www\./, "").trim();
}

// Listen for messages from content scripts or popup
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  // 1. Query credentials for automatic autofill
  if (message.type === "QUERY_CREDENTIALS") {
    (async () => {
      try {
        const config = await chrome.storage.local.get(["serverUrl", "autoFill"]);
        if (config.autoFill === false) {
          sendResponse({ authenticated: false, autoFillDisabled: true, secrets: [] });
          return;
        }

        const serverUrl = (config.serverUrl || "https://clip.codesky.tech").replace(/\/+$/, "");
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

        const res = await fetch(`${serverUrl}/api/secrets/lookup?url=${encodeURIComponent(host)}`, {
          headers: { Authorization: `Bearer ${token}` },
        });

        if (!res.ok) {
          sendResponse({ authenticated: true, secrets: [] });
          return;
        }

        const data = await res.json();
        sendResponse({ authenticated: true, secrets: data.secrets || [] });
      } catch (err) {
        sendResponse({ authenticated: false, error: err.message, secrets: [] });
      }
    })();
    return true; // async response
  }

  // 2. Automatic saving and updating of credentials without popup interaction
  if (message.type === "AUTO_SAVE_CREDENTIAL") {
    (async () => {
      try {
        const config = await chrome.storage.local.get(["serverUrl", "autoSave"]);
        const serverUrl = (config.serverUrl || "https://clip.codesky.tech").replace(/\/+$/, "");
        const token = await getAuthToken(serverUrl);

        const username = (message.username || "").trim();
        const password = message.password || "";
        const url = message.url || "";
        const hostname = cleanHost(message.hostname || "");

        if (!password) {
          sendResponse({ success: false, reason: "empty_password" });
          return;
        }

        // Cache in pendingSave as fallback
        await chrome.storage.local.set({
          pendingSave: {
            url,
            username,
            password,
            title: message.title || hostname,
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

        // Lookup existing secrets for this domain
        const lookupRes = await fetch(`${serverUrl}/api/secrets/lookup?url=${encodeURIComponent(hostname)}`, {
          headers: { Authorization: `Bearer ${token}` },
        });

        let existing = null;
        if (lookupRes.ok) {
          const lookupData = await lookupRes.json();
          const list = lookupData.secrets || [];
          if (username) {
            existing = list.find((s) => (s.username || "").toLowerCase() === username.toLowerCase());
          }
          if (!existing && list.length === 1 && !username) {
            existing = list[0];
          }
        }

        if (existing) {
          // If password unchanged, nothing to update
          if (existing.value === password) {
            sendResponse({ success: true, status: "unchanged", title: existing.title });
            return;
          }

          // Update existing password
          const updateRes = await fetch(`${serverUrl}/api/secrets/${existing.id}`, {
            method: "PUT",
            headers: {
              "Content-Type": "application/json",
              Authorization: `Bearer ${token}`,
            },
            body: JSON.stringify({
              title: existing.title,
              kind: existing.kind || "password",
              username: username || existing.username,
              url: existing.url || url,
              value: password,
              notes: existing.notes || "",
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

        // Brand new credential: Auto-create in vault
        const title = hostname ? `${hostname.charAt(0).toUpperCase() + hostname.slice(1)} Account` : (message.title || "New Account");
        const createRes = await fetch(`${serverUrl}/api/secrets`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${token}`,
          },
          body: JSON.stringify({
            title,
            kind: "password",
            username,
            url,
            value: password,
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
    return true; // async response
  }

  // 3. Fallback manual credential submitted
  if (message.type === "CREDENTIAL_SUBMITTED") {
    chrome.storage.local.set({
      pendingSave: {
        url: message.url,
        username: message.username,
        password: message.password,
        title: message.title || "",
        timestamp: Date.now(),
      },
    });
    sendResponse({ ok: true });
    return true;
  }
});
