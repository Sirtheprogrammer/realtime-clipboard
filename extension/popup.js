function showUpdateBanner(info) {
  const banner = $("updateBanner");
  if (!banner) return;
  banner.hidden = false;
  banner.style.display = "block";
  $("updateVerText").textContent = `v${info.version} is available`;
  $("updateDownloadLink").href = info.downloadUrl || info.releaseUrl || "#";
}

/* ============================================================
   Clipboard Vault — Extension Popup Logic
   Manages credentials, autofill, and active tab domain syncing
   ============================================================ */

const $ = (id) => document.getElementById(id);

let state = {
  serverUrl: "https://clip.codesky.tech",
  token: null,
  user: null,
  activeTab: null,
  activeDomain: "",
  matchingSecrets: [],
  allSecrets: [],
};

document.addEventListener("DOMContentLoaded", async () => {
  await loadStoredConfig();
  wireUI();
  await checkServerHealth();
  await detectActiveTab();

  if (state.token) {
    await verifyAuth();
  } else {
    showAuthView();
    // Attempt automatic cookie sync in case user logged in via web/GitHub
    await attemptCookieSync();
  }
});

// Re-check when popup gains focus (e.g. returning from GitHub OAuth tab)
window.addEventListener("focus", async () => {
  if (!state.token) {
    await attemptCookieSync();
  }
});

/* ───────────────────────── Config & State ───────────────────────── */

async function loadStoredConfig() {
  const data = await chrome.storage.local.get(["serverUrl", "token", "pendingSave", "autoFill", "autoSave"]);
  if (data.serverUrl && data.serverUrl !== "http://localhost:8080") {
    state.serverUrl = data.serverUrl.replace(/\/+$/, "");
  } else {
    state.serverUrl = "https://clip.codesky.tech";
    await chrome.storage.local.set({ serverUrl: state.serverUrl });
  }
  if (data.token) state.token = data.token;
  $("serverUrlInput").value = state.serverUrl;

  // Version & Updates
  const currentVer = chrome.runtime.getManifest().version;
  if ($("extCurrentVersion")) $("extCurrentVersion").textContent = `v${currentVer}`;
  const updateData = await chrome.storage.local.get(["updateInfo"]);
  if (updateData.updateInfo && updateData.updateInfo.available) {
    showUpdateBanner(updateData.updateInfo);
  }
  if ($("toggleAutoFill")) $("toggleAutoFill").checked = data.autoFill !== false;
  if ($("toggleAutoSave")) $("toggleAutoSave").checked = data.autoSave !== false;

  if (data.pendingSave && data.pendingSave.url) {
    showPendingBanner(data.pendingSave);
  }
}

async function checkServerHealth() {
  const dot = $("serverStatusDot");
  try {
    const res = await fetch(`${state.serverUrl}/api/health`);
    if (res.ok) {
      dot.className = "status-dot connected";
      dot.title = "Connected to Clipboard Server";
    } else {
      dot.className = "status-dot error";
      dot.title = "Server returned error";
    }
  } catch {
    dot.className = "status-dot error";
    dot.title = "Could not reach Clipboard Server";
  }
}

async function detectActiveTab() {
  const tabs = await chrome.tabs.query({ active: true, currentWindow: true });
  if (tabs && tabs[0]) {
    state.activeTab = tabs[0];
    try {
      const u = new URL(tabs[0].url);
      if (u.protocol.startsWith("http")) {
        state.activeDomain = u.hostname.replace(/^www\./, "");
      }
    } catch {
      state.activeDomain = "";
    }
  }
  $("currentDomain").textContent = state.activeDomain || "No active web page";
}

/* ───────────────────────── Auth ───────────────────────── */

async function attemptCookieSync() {
  try {
    const res = await fetch(`${state.serverUrl}/api/auth/me`, {
      credentials: "include",
    });
    if (res.ok) {
      const data = await res.json();
      if (data.token && data.user) {
        state.token = data.token;
        state.user = data.user;
        await chrome.storage.local.set({ token: data.token });
        showVaultView();
        await loadDomainSecrets();
        await loadAllSecrets();
        showToast("Connected to active session!");
        return;
      }
    }
  } catch {
    // silent
  }
  showAuthView();
}

async function verifyAuth() {
  try {
    const res = await fetch(`${state.serverUrl}/api/auth/me`, {
      headers: { Authorization: `Bearer ${state.token}` },
    });
    if (!res.ok) throw new Error("Session expired");

    const data = await res.json();
    state.user = data.user;
    showVaultView();
    await loadDomainSecrets();
    await loadAllSecrets();
  } catch {
    state.token = null;
    state.user = null;
    await chrome.storage.local.remove(["token"]);
    showAuthView();
  }
}

function showAuthView() {
  $("authView").hidden = false;
  $("vaultView").hidden = true;
}

function showVaultView() {
  $("authView").hidden = true;
  $("vaultView").hidden = false;
  $("userDisplay").textContent = state.user.github_user ? `@${state.user.github_user}` : state.user.email;
}

/* ───────────────────────── Secrets Loading & Actions ───────────────────────── */

async function loadDomainSecrets() {
  if (!state.activeDomain) {
    $("matchingSecretsList").innerHTML = "";
    $("noMatchingSecrets").hidden = false;
    return;
  }

  try {
    const res = await fetch(`${state.serverUrl}/api/secrets/lookup?url=${encodeURIComponent(state.activeDomain)}`, {
      headers: { Authorization: `Bearer ${state.token}` },
    });
    if (!res.ok) return;

    const data = await res.json();
    state.matchingSecrets = data.secrets || [];
    renderMatchingSecrets();
  } catch (err) {
    showToast("Failed to fetch domain credentials");
  }
}

function renderMatchingSecrets() {
  const container = $("matchingSecretsList");
  const empty = $("noMatchingSecrets");
  container.innerHTML = "";

  if (state.matchingSecrets.length === 0) {
    empty.hidden = false;
    return;
  }
  empty.hidden = true;

  state.matchingSecrets.forEach((sec) => {
    const card = document.createElement("div");
    card.className = "cred-card";
    card.innerHTML = `
      <div class="cred-top">
        <b class="cred-title">${escapeHtml(sec.title)}</b>
        <span class="cred-user">${escapeHtml(sec.username || "—")}</span>
      </div>
      <div class="cred-actions">
        <button class="ext-btn ext-btn-primary sm act-autofill">Autofill</button>
        <button class="ext-btn ext-btn-ghost sm act-copy-user" title="Copy username">User</button>
        <button class="ext-btn ext-btn-ghost sm act-copy-pass" title="Copy password">Pass</button>
      </div>
    `;

    // Autofill
    card.querySelector(".act-autofill").addEventListener("click", () => {
      triggerAutofill(sec.username, sec.value);
    });

    // Copy user
    card.querySelector(".act-copy-user").addEventListener("click", async () => {
      await navigator.clipboard.writeText(sec.username || "");
      showToast("Username copied!");
    });

    // Copy pass
    card.querySelector(".act-copy-pass").addEventListener("click", async () => {
      await navigator.clipboard.writeText(sec.value || "");
      showToast("Password copied!");
    });

    container.appendChild(card);
  });
}

function triggerAutofill(username, password) {
  if (!state.activeTab || !state.activeTab.id) {
    showToast("No active page to autofill");
    return;
  }

  chrome.tabs.sendMessage(
    state.activeTab.id,
    { type: "AUTOFILL", username, password },
    (response) => {
      if (chrome.runtime.lastError || !response || !response.success) {
        // Content script might not be injected yet, attempt programmatic injection
        chrome.scripting.executeScript(
          {
            target: { tabId: state.activeTab.id },
            files: ["content.js"],
          },
          () => {
            chrome.tabs.sendMessage(state.activeTab.id, { type: "AUTOFILL", username, password });
            showToast("Credentials autofilled!");
          }
        );
      } else {
        showToast("Credentials autofilled!");
      }
    }
  );
}

async function loadAllSecrets() {
  try {
    const res = await fetch(`${state.serverUrl}/api/secrets`, {
      headers: { Authorization: `Bearer ${state.token}` },
    });
    if (!res.ok) return;

    const data = await res.json();
    state.allSecrets = data.secrets || [];
    renderAllSecrets("");
  } catch {
    // silent
  }
}

function renderAllSecrets(filterQuery) {
  const container = $("allSecretsList");
  container.innerHTML = "";

  const filtered = state.allSecrets.filter((sec) => {
    if (!filterQuery) return true;
    const q = filterQuery.toLowerCase();
    return (
      (sec.title || "").toLowerCase().includes(q) ||
      (sec.username || "").toLowerCase().includes(q) ||
      (sec.url || "").toLowerCase().includes(q)
    );
  });

  filtered.slice(0, 15).forEach((sec) => {
    const item = document.createElement("div");
    item.className = "search-item";
    item.innerHTML = `
      <div class="search-item-info">
        <b>${escapeHtml(sec.title)}</b>
        <span>${escapeHtml(sec.username || sec.url || "—")}</span>
      </div>
      <div class="search-item-tools">
        <button class="ext-btn ext-btn-ghost sm act-copy-pass" title="Copy Secret">Copy</button>
      </div>
    `;

    item.querySelector(".act-copy-pass").addEventListener("click", async () => {
      await navigator.clipboard.writeText(sec.value || "");
      showToast(`Copied ${sec.title}`);
    });

    container.appendChild(item);
  });
}

/* ───────────────────────── Pending Save ───────────────────────── */

function showPendingBanner(pending) {
  const banner = $("pendingSaveBanner");
  banner.hidden = false;
  $("pendingUserText").textContent = pending.username ? `${pending.username} on ${cleanDomain(pending.url)}` : cleanDomain(pending.url);

  $("acceptPendingBtn").onclick = async () => {
    openSecretModal({
      title: pending.title || cleanDomain(pending.url),
      username: pending.username,
      url: pending.url,
      value: pending.password,
    });
    await chrome.storage.local.remove(["pendingSave"]);
    banner.hidden = true;
  };

  $("dismissPendingBtn").onclick = async () => {
    await chrome.storage.local.remove(["pendingSave"]);
    banner.hidden = true;
  };
}

/* ───────────────────────── Wire UI ───────────────────────── */

function wireUI() {
  // Settings toggle
  $("settingsToggleBtn").addEventListener("click", () => {
    $("settingsPanel").hidden = !$("settingsPanel").hidden;
  });

  // Automation toggles
  $("toggleAutoFill")?.addEventListener("change", async (e) => {
    await chrome.storage.local.set({ autoFill: e.target.checked });
    showToast(e.target.checked ? "Auto-fill enabled" : "Auto-fill disabled");
  });
  $("toggleAutoSave")?.addEventListener("change", async (e) => {
    await chrome.storage.local.set({ autoSave: e.target.checked });
    showToast(e.target.checked ? "Auto-save enabled" : "Auto-save disabled");
  });

  // Check for updates
  $("checkUpdatesBtn")?.addEventListener("click", () => {
    showToast("Checking for updates...");
    chrome.runtime.sendMessage({ type: "CHECK_FOR_UPDATES" }, (res) => {
      if (res && res.updateAvailable) {
        showUpdateBanner(res.updateInfo);
        showToast(`Update v${res.updateInfo.version} available!`);
      } else {
        showToast("Extension is up to date");
      }
    });
  });

  // Import Browser Passwords
  $("extImportBtn")?.addEventListener("click", () => {
    if (!state.token) {
      showToast("Please sign in first to import passwords");
      showAuthView();
      return;
    }
    $("extCsvFile")?.click();
  });

  $("extCsvFile")?.addEventListener("change", async (e) => {
    if (!e.target.files?.length) return;
    const file = e.target.files[0];
    const reader = new FileReader();
    reader.onload = async (ev) => {
      try {
        const text = ev.target.result;
        const parsed = parseClientCSV(text);
        if (!parsed.length) {
          showToast("No passwords found in CSV file");
          return;
        }

        showToast(`Importing ${parsed.length} passwords...`);
        const res = await fetch(`${state.serverUrl}/api/secrets/import`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${state.token}`,
          },
          body: JSON.stringify({ secrets: parsed }),
        });

        const data = await res.json();
        if (!res.ok) throw new Error(data.error || "Import failed");

        showToast(`Imported ${data.imported} passwords!`);
        await loadDomainSecrets();
        await loadAllSecrets();
      } catch (err) {
        showToast(err.message || "Failed to import CSV");
      } finally {
        $("extCsvFile").value = "";
      }
    };
    reader.readAsText(file);
  });

  // Save server url
  $("saveServerBtn").addEventListener("click", async () => {
    const newUrl = $("serverUrlInput").value.trim().replace(/\/+$/, "");
    if (!newUrl) return;
    state.serverUrl = newUrl;
    await chrome.storage.local.set({ serverUrl: newUrl });
    await checkServerHealth();
    showToast("Server URL updated");
    if (state.token) await verifyAuth();
  });

  // Login form
  $("extLoginForm").addEventListener("submit", async (e) => {
    e.preventDefault();
    const email = $("loginEmail").value.trim();
    const password = $("loginPassword").value;
    const btn = $("loginBtn");
    btn.disabled = true;

    try {
      const res = await fetch(`${state.serverUrl}/api/auth/login`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });

      const data = await res.json();
      if (!res.ok) throw new Error(data.error || "Login failed");

      state.token = data.token;
      state.user = data.user;
      await chrome.storage.local.set({ token: data.token });
      showToast("Signed in!");
      showVaultView();
      await loadDomainSecrets();
      await loadAllSecrets();
    } catch (err) {
      showToast(err.message);
    } finally {
      btn.disabled = false;
    }
  });

  // GitHub Auth in extension
  $("extGithubAuthBtn").addEventListener("click", () => {
    chrome.tabs.create({ url: `${state.serverUrl}/api/auth/github` });
    showToast("Authorize with GitHub in the new tab");
  });

  // Open web vault
  $("openWebVaultLink").addEventListener("click", (e) => {
    e.preventDefault();
    chrome.tabs.create({ url: state.serverUrl });
  });

  // Logout
  $("logoutBtn").addEventListener("click", async () => {
    try {
      await fetch(`${state.serverUrl}/api/auth/logout`, {
        method: "POST",
        headers: { Authorization: `Bearer ${state.token}` },
      });
    } catch {
      // silent
    }
    state.token = null;
    state.user = null;
    await chrome.storage.local.remove(["token"]);
    showAuthView();
    showToast("Signed out");
  });

  // Add for site button
  $("addForSiteBtn").addEventListener("click", () => {
    openSecretModal({
      title: state.activeDomain ? `${state.activeDomain} Account` : "New Secret",
      username: "",
      url: state.activeTab ? state.activeTab.url : "",
      value: "",
    });
  });

  // Search input
  $("allSecretsSearch").addEventListener("input", (e) => {
    renderAllSecrets(e.target.value.trim());
  });

  // Modal actions
  $("closeSecretModal")?.addEventListener("click", closeSecretModal);
  $("cancelSecretBtn")?.addEventListener("click", closeSecretModal);
  $("secretModal")?.addEventListener("click", (e) => {
    if (e.target === $("secretModal")) closeSecretModal();
  });

  // Generate password
  $("extGenPassBtn").addEventListener("click", () => {
    const pwd = generateSecurePassword();
    $("newValue").value = pwd;
    showToast("Generated strong password");
  });


  // Password modal trigger
  $("extOpenPwdBtn")?.addEventListener("click", () => {
    $("settingsPanel").hidden = true;
    openPwdModal();
  });
  $("closePwdModal")?.addEventListener("click", closePwdModal);
  $("cancelPwdModalBtn")?.addEventListener("click", closePwdModal);
  $("extPasswordModal")?.addEventListener("click", (e) => {
    if (e.target === $("extPasswordModal")) closePwdModal();
  });

  // Set / Change Password Form
  $("extSetPwdForm")?.addEventListener("submit", async (e) => {
    e.preventDefault();
    const curPwd = $("extCurPwdInput") ? $("extCurPwdInput").value : "";
    const newPwd = $("extNewPwdInput").value;
    const confirmPwd = $("extConfirmPwdInput").value;
    const saveBtn = $("saveExtPwdBtn");

    if (newPwd !== confirmPwd) {
      showToast("Passwords do not match");
      return;
    }
    if (newPwd.length < 8) {
      showToast("Password must be at least 8 characters");
      return;
    }

    saveBtn.disabled = true;
    try {
      const res = await fetch(`${state.serverUrl}/api/auth/password`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${state.token}`,
        },
        body: JSON.stringify({
          current_password: curPwd,
          new_password: newPwd,
        }),
      });

      const data = await res.json();
      if (!res.ok) throw new Error(data.error || "Failed to update password");

      if (state.user) {
        state.user.has_password = true;
      }
      showToast("Password saved successfully!");
      closePwdModal();
    } catch (err) {
      showToast(err.message);
    } finally {
      saveBtn.disabled = false;
    }
  });

  // Save new secret form
  $("newSecretForm").addEventListener("submit", async (e) => {
    e.preventDefault();
    const title = $("newTitle").value.trim();
    const username = $("newUsername").value.trim();
    const url = $("newURL").value.trim();
    const value = $("newValue").value;
    const saveBtn = $("saveSecretBtn");
    saveBtn.disabled = true;

    const kind = $("newKind") ? $("newKind").value : "password";

    try {
      const res = await fetch(`${state.serverUrl}/api/secrets`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${state.token}`,
        },
        body: JSON.stringify({ title, kind, username, url, value }),
      });

      const data = await res.json();
      if (!res.ok) throw new Error(data.error || "Failed to save secret");

      showToast("Encrypted and saved!");
      closeSecretModal();
      await loadDomainSecrets();
      await loadAllSecrets();
    } catch (err) {
      showToast(err.message);
    } finally {
      saveBtn.disabled = false;
    }
  });
}

function openSecretModal(prefill = {}) {
  if ($("newKind")) $("newKind").value = prefill.kind || "password";
  $("newTitle").value = prefill.title || "";
  $("newUsername").value = prefill.username || "";
  $("newURL").value = prefill.url || "";
  $("newValue").value = prefill.value || "";
  const modal = $("secretModal");
  if (modal) {
    modal.hidden = false;
    modal.style.display = "grid";
  }
}

function closeSecretModal() {
  const modal = $("secretModal");
  if (modal) {
    modal.hidden = true;
    modal.style.display = "none";
  }
}

/* ───────────────────────── Helpers ───────────────────────── */

function showToast(msg) {
  const toast = $("toast");
  toast.textContent = msg;
  toast.hidden = false;
  setTimeout(() => {
    toast.hidden = true;
  }, 2200);
}

function cleanDomain(urlStr) {
  if (!urlStr) return "";
  try {
    const u = new URL(urlStr.startsWith("http") ? urlStr : `https://${urlStr}`);
    return u.hostname.replace(/^www\./, "");
  } catch {
    return urlStr;
  }
}

function escapeHtml(str) {
  return (str || "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}

function generateSecurePassword(length = 24) {
  const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}";
  const array = new Uint32Array(length);
  window.crypto.getRandomValues(array);
  let result = "";
  for (let i = 0; i < length; i++) {
    result += chars[array[i] % chars.length];
  }
  return result;
}

function parseClientCSV(text) {
  const lines = text.split(/\r?\n/).filter((l) => l.trim().length > 0);
  if (lines.length < 2) return [];

  const tokenizeLine = (line) => {
    const tokens = [];
    let current = "";
    let insideQuotes = false;
    for (let i = 0; i < line.length; i++) {
      const char = line[i];
      if (char === '"') {
        if (insideQuotes && line[i + 1] === '"') {
          current += '"';
          i++;
        } else {
          insideQuotes = !insideQuotes;
        }
      } else if (char === "," && !insideQuotes) {
        tokens.push(current.trim());
        current = "";
      } else {
        current += char;
      }
    }
    tokens.push(current.trim());
    return tokens.map((t) => t.replace(/^["']|["']$/g, "").trim());
  };

  const headers = tokenizeLine(lines[0]).map((h) => h.toLowerCase());

  const colIdx = (...candidates) => {
    for (const c of candidates) {
      const idx = headers.indexOf(c);
      if (idx !== -1) return idx;
    }
    return -1;
  };

  const titleIdx = colIdx("name", "title", "folder");
  const urlIdx = colIdx("url", "login_uri", "uri", "website", "formactionorigin");
  const userIdx = colIdx("username", "login_username", "user", "login", "email");
  const passIdx = colIdx("password", "login_password", "pass");
  const notesIdx = colIdx("notes", "note");

  if (passIdx === -1) return [];

  const results = [];
  for (let i = 1; i < lines.length; i++) {
    const cols = tokenizeLine(lines[i]);
    const password = cols[passIdx];
    if (!password) continue;

    const title = titleIdx !== -1 ? cols[titleIdx] : "";
    const url = urlIdx !== -1 ? cols[urlIdx] : "";
    const username = userIdx !== -1 ? cols[userIdx] : "";
    const notes = notesIdx !== -1 ? cols[notesIdx] : "";

    results.push({
      title: title || cleanDomain(url) || "Imported Account",
      url: url || "",
      username: username || "",
      value: password,
      notes: notes || "",
      kind: "password",
    });
  }

  return results;
}

function openPwdModal() {
  const modal = $("extPasswordModal");
  if (!modal) return;
  if (!state.token) {
    showToast("Please sign in first");
    return;
  }
  const hasPwd = state.user && state.user.has_password;
  const curGroup = $("extCurrentPwdGroup");
  const newLabel = $("extNewPwdLabel");
  const saveBtn = $("saveExtPwdBtn");
  const hint = $("extPwdModalHint");

  if (curGroup && newLabel && saveBtn) {
    if (hasPwd) {
      curGroup.hidden = false;
      newLabel.textContent = "New Password (min 8 characters)";
      saveBtn.textContent = "Change Password";
      if (hint) hint.textContent = "Enter your current password to set a new password.";
    } else {
      curGroup.hidden = true;
      newLabel.textContent = "Account Password (min 8 characters)";
      saveBtn.textContent = "Set Account Password";
      if (hint) hint.textContent = "Set a password to authenticate with email + password on any device.";
    }
  }

  modal.hidden = false;
  modal.style.display = "grid";
}

function closePwdModal() {
  const modal = $("extPasswordModal");
  if (modal) {
    modal.hidden = true;
    modal.style.display = "none";
  }
  if ($("extCurPwdInput")) $("extCurPwdInput").value = "";
  if ($("extNewPwdInput")) $("extNewPwdInput").value = "";
  if ($("extConfirmPwdInput")) $("extConfirmPwdInput").value = "";
}
