/* ============================================================
   Clipboard — Secrets Vault & Authentication Client
   Manages user accounts, GitHub OAuth, and encrypted secrets
   ============================================================ */

const $ = (id) => document.getElementById(id);

export const vaultState = {
  user: null,
  secrets: [],
  activeFilter: "all",
  searchQuery: "",
  editingSecretId: null,
};

export async function initVault(api, toast) {
  wireAuthEvents(api, toast);
  wireVaultEvents(api, toast);
  await checkCurrentUser(api);

  // Check URL params for OAuth redirect (?auth=success)
  const urlParams = new URLSearchParams(window.location.search);
  if (urlParams.get("auth") === "success") {
    toast("Signed in successfully via GitHub!");
    window.history.replaceState({}, "", window.location.pathname);
    await checkCurrentUser(api);
    openVaultModal(api, toast);
  }
}

async function checkCurrentUser(api) {
  try {
    const res = await api("/api/auth/me");
    vaultState.user = res.user;
    updateAuthUI();
  } catch {
    vaultState.user = null;
    updateAuthUI();
  }
}

function updateAuthUI() {
  const authLabels = document.querySelectorAll(".auth-label");
  const authBtns = document.querySelectorAll(".auth-btn");
  const authDots = document.querySelectorAll(".auth-indicator");

  if (vaultState.user) {
    const display = vaultState.user.github_user || vaultState.user.email.split("@")[0];
    authLabels.forEach((el) => (el.textContent = display));
    authBtns.forEach((btn) => {
      btn.setAttribute("title", `Signed in as ${vaultState.user.email}`);
      btn.classList.add("is-authenticated");
    });
    authDots.forEach((dot) => (dot.hidden = false));
  } else {
    authLabels.forEach((el) => (el.textContent = "Sign In"));
    authBtns.forEach((btn) => {
      btn.setAttribute("title", "Sign in or create account");
      btn.classList.remove("is-authenticated");
    });
    authDots.forEach((dot) => (dot.hidden = true));
  }
}

/* ───────────────────────── Auth Dialog ───────────────────────── */

function wireAuthEvents(api, toast) {
  const authBtns = document.querySelectorAll(".auth-btn");
  authBtns.forEach((btn) => {
    btn.addEventListener("click", () => {
      openAuthModal();
    });
  });

  $("authClose")?.addEventListener("click", closeAuthModal);
  $("authModal")?.addEventListener("click", (e) => {
    if (e.target === $("authModal")) closeAuthModal();
  });

  // Tab switching
  $("tabSignIn")?.addEventListener("click", () => switchAuthTab("signin"));
  $("tabRegister")?.addEventListener("click", () => switchAuthTab("register"));

  // Forms
  $("signInForm")?.addEventListener("submit", async (e) => {
    e.preventDefault();
    const email = $("signInEmail").value.trim();
    const password = $("signInPassword").value;
    const btn = $("signInSubmit");
    btn.disabled = true;
    try {
      const res = await api("/api/auth/login", {
        method: "POST",
        body: JSON.stringify({ email, password }),
      });
      vaultState.user = res.user;
      updateAuthUI();
      closeAuthModal();
      toast(`Welcome back, ${vaultState.user.email}!`);
      openVaultModal(api, toast);
    } catch (err) {
      toast(err.message, "error");
    } finally {
      btn.disabled = false;
    }
  });

  $("registerForm")?.addEventListener("submit", async (e) => {
    e.preventDefault();
    const email = $("registerEmail").value.trim();
    const password = $("registerPassword").value;
    const btn = $("registerSubmit");
    btn.disabled = true;
    try {
      const res = await api("/api/auth/register", {
        method: "POST",
        body: JSON.stringify({ email, password }),
      });
      vaultState.user = res.user;
      updateAuthUI();
      closeAuthModal();
      toast("Account created successfully!");
      openVaultModal(api, toast);
    } catch (err) {
      toast(err.message, "error");
    } finally {
      btn.disabled = false;
    }
  });

  // GitHub Auth
  $("githubAuthBtn")?.addEventListener("click", () => {
    window.location.href = "/api/auth/github";
  });
  $("githubRegisterBtn")?.addEventListener("click", () => {
    window.location.href = "/api/auth/github";
  });

  // Sign out
  $("authSignOutBtn")?.addEventListener("click", async () => {
    try {
      await api("/api/auth/logout", { method: "POST" });
      vaultState.user = null;
      vaultState.secrets = [];
      updateAuthUI();
      closeAuthModal();
      closeVaultModal();
      toast("Signed out");
    } catch (err) {
      toast(err.message, "error");
    }
  });

  // Set / Change Password
  $("setPasswordForm")?.addEventListener("submit", async (e) => {
    e.preventDefault();
    const curPwd = $("accCurrentPassword") ? $("accCurrentPassword").value : "";
    const newPwd = $("accNewPassword").value;
    const confirmPwd = $("accConfirmPassword").value;
    const saveBtn = $("savePasswordBtn");

    if (newPwd !== confirmPwd) {
      toast("Passwords do not match", "error");
      return;
    }
    if (newPwd.length < 8) {
      toast("Password must be at least 8 characters", "error");
      return;
    }

    saveBtn.disabled = true;
    try {
      const res = await api("/api/auth/password", {
        method: "POST",
        body: JSON.stringify({
          current_password: curPwd,
          new_password: newPwd,
        }),
      });

      toast(res.message || "Password saved successfully!");
      if (vaultState.user) {
        vaultState.user.has_password = true;
      }
      $("accNewPassword").value = "";
      $("accConfirmPassword").value = "";
      if ($("accCurrentPassword")) $("accCurrentPassword").value = "";
      openAuthModal();
    } catch (err) {
      toast(err.message, "error");
    } finally {
      saveBtn.disabled = false;
    }
  });
}

function openAuthModal() {
  const modal = $("authModal");
  if (!modal) return;

  if (vaultState.user) {
    // Show signed-in view
    $("authLoggedOutView").hidden = true;
    $("authLoggedInView").hidden = false;
    $("authProfileEmail").textContent = vaultState.user.email;
    const ghRow = $("authProfileGitHub");
    if (vaultState.user.github_user) {
      ghRow.hidden = false;
      $("authProfileGitHubUser").textContent = `@${vaultState.user.github_user}`;
    } else {
      ghRow.hidden = true;
    }

    // Refresh password security card state
    const hasPwd = !!vaultState.user.has_password;
    const badge = $("pwdStatusBadge");
    const desc = $("pwdStatusDesc");
    const curGroup = $("currentPwdGroup");
    const newLabel = $("newPwdLabel");
    const saveBtn = $("savePasswordBtn");

    if (badge && desc && curGroup && newLabel && saveBtn) {
      if (hasPwd) {
        badge.textContent = "Protected";
        badge.className = "chip-sub badge-success";
        desc.textContent = "You can sign in using your email & password or via GitHub OAuth.";
        curGroup.hidden = false;
        newLabel.textContent = "New Password (min 8 characters)";
        saveBtn.innerHTML = '<svg width="14" height="14"><use href="#i-shield"/></svg> Change Password';
      } else {
        badge.textContent = "Not Set (GitHub Only)";
        badge.className = "chip-sub badge-warning";
        desc.textContent = "You signed in via GitHub. Set an account password to also sign in directly with your email & password on mobile and browser extensions.";
        curGroup.hidden = true;
        newLabel.textContent = "Account Password (min 8 characters)";
        saveBtn.innerHTML = '<svg width="14" height="14"><use href="#i-shield"/></svg> Set Account Password';
      }
    }
  } else {
    // Show login tabs
    $("authLoggedOutView").hidden = false;
    $("authLoggedInView").hidden = true;
    switchAuthTab("signin");
  }

  modal.hidden = false;
}

function closeAuthModal() {
  const modal = $("authModal");
  if (modal) modal.hidden = true;
}

function switchAuthTab(tab) {
  const tabSignIn = $("tabSignIn");
  const tabRegister = $("tabRegister");
  const signInForm = $("signInForm");
  const registerForm = $("registerForm");

  if (tab === "signin") {
    tabSignIn?.classList.add("is-active");
    tabRegister?.classList.remove("is-active");
    if (signInForm) signInForm.hidden = false;
    if (registerForm) registerForm.hidden = true;
  } else {
    tabRegister?.classList.add("is-active");
    tabSignIn?.classList.remove("is-active");
    if (registerForm) registerForm.hidden = false;
    if (signInForm) signInForm.hidden = true;
  }
}

/* ───────────────────────── Secrets Vault ───────────────────────── */

function wireVaultEvents(api, toast) {
  const vaultBtns = document.querySelectorAll(".vault-btn");
  vaultBtns.forEach((btn) => {
    btn.addEventListener("click", () => {
      if (!vaultState.user) {
        openAuthModal();
        toast("Please sign in to access your encrypted Secret Store");
        return;
      }
      openVaultModal(api, toast);
    });
  });

  // Extension Guide Modal wiring
  document.querySelectorAll(".ext-guide-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
      const m = $("extModal");
      if (m) m.hidden = false;
    });
  });
  $("extModalClose")?.addEventListener("click", () => {
    const m = $("extModal");
    if (m) m.hidden = true;
  });
  $("extModal")?.addEventListener("click", (e) => {
    if (e.target === $("extModal")) $("extModal").hidden = true;
  });

  $("vaultClose")?.addEventListener("click", closeVaultModal);
  $("vaultModal")?.addEventListener("click", (e) => {
    if (e.target === $("vaultModal")) closeVaultModal();
  });

  // Filter chips
  const filterChips = document.querySelectorAll("#vaultFilters .chip");
  filterChips.forEach((chip) => {
    chip.addEventListener("click", () => {
      filterChips.forEach((c) => c.classList.remove("is-active"));
      chip.classList.add("is-active");
      vaultState.activeFilter = chip.dataset.kind || "all";
      renderSecretsList(toast);
    });
  });

  // Search
  $("vaultSearch")?.addEventListener("input", (e) => {
    vaultState.searchQuery = e.target.value.trim().toLowerCase();
    renderSecretsList(toast);
  });

  wireImportEvents(api, toast);

  // New Secret Button
  $("newSecretBtn")?.addEventListener("click", () => {
    openSecretDrawer(null);
  });

  // Drawer events
  $("secretDrawerClose")?.addEventListener("click", closeSecretDrawer);
  $("secretDrawerModal")?.addEventListener("click", (e) => {
    if (e.target === $("secretDrawerModal")) closeSecretDrawer();
  });

  // Password Generator
  $("genPasswordBtn")?.addEventListener("click", () => {
    const pwd = generateSecurePassword();
    $("secretValue").value = pwd;
    $("secretValue").type = "text";
    $("secretValueToggle").querySelector("use").setAttribute("href", "#i-eye-off");
    toast("Generated strong 24-character password");
  });

  // Show/Hide password toggle
  $("secretValueToggle")?.addEventListener("click", () => {
    const input = $("secretValue");
    const use = $("secretValueToggle").querySelector("use");
    if (input.type === "password") {
      input.type = "text";
      use.setAttribute("href", "#i-eye-off");
    } else {
      input.type = "password";
      use.setAttribute("href", "#i-eye");
    }
  });

  // Save Secret Form
  $("secretForm")?.addEventListener("submit", async (e) => {
    e.preventDefault();
    const title = $("secretTitle").value.trim();
    const kind = $("secretKind").value;
    const username = $("secretUsername").value.trim();
    const url = $("secretURL").value.trim();
    const value = $("secretValue").value;
    const notes = $("secretNotes").value.trim();

    if (!title) {
      toast("Title is required", "error");
      return;
    }

    const saveBtn = $("secretSaveBtn");
    saveBtn.disabled = true;

    try {
      if (vaultState.editingSecretId) {
        const res = await api(`/api/secrets/${vaultState.editingSecretId}`, {
          method: "PUT",
          body: JSON.stringify({ title, kind, username, url, value, notes }),
        });
        const idx = vaultState.secrets.findIndex((s) => s.id === vaultState.editingSecretId);
        if (idx !== -1) vaultState.secrets[idx] = res.secret;
        toast("Secret updated successfully");
      } else {
        const res = await api("/api/secrets", {
          method: "POST",
          body: JSON.stringify({ title, kind, username, url, value, notes }),
        });
        vaultState.secrets.unshift(res.secret);
        toast("Secret securely encrypted and saved");
      }

      closeSecretDrawer();
      renderSecretsList(toast);
    } catch (err) {
      toast(err.message, "error");
    } finally {
      saveBtn.disabled = false;
    }
  });
}

export async function openVaultModal(api, toast) {
  const modal = $("vaultModal");
  if (!modal) return;
  modal.hidden = false;

  try {
    const res = await api("/api/secrets");
    vaultState.secrets = res.secrets || [];
    renderSecretsList(toast);
  } catch (err) {
    toast(err.message, "error");
  }
}

function closeVaultModal() {
  const modal = $("vaultModal");
  if (modal) modal.hidden = true;
}

function renderSecretsList(toast) {
  const container = $("secretsList");
  const empty = $("secretsEmpty");
  if (!container) return;

  container.innerHTML = "";

  const filtered = vaultState.secrets.filter((sec) => {
    if (vaultState.activeFilter !== "all" && sec.kind !== vaultState.activeFilter) {
      return false;
    }
    if (vaultState.searchQuery) {
      const q = vaultState.searchQuery;
      const matchTitle = (sec.title || "").toLowerCase().includes(q);
      const matchUser = (sec.username || "").toLowerCase().includes(q);
      const matchURL = (sec.url || "").toLowerCase().includes(q);
      const matchNotes = (sec.notes || "").toLowerCase().includes(q);
      if (!matchTitle && !matchUser && !matchURL && !matchNotes) return false;
    }
    return true;
  });

  if (filtered.length === 0) {
    if (empty) empty.hidden = false;
    return;
  }
  if (empty) empty.hidden = true;

  filtered.forEach((sec) => {
    const card = document.createElement("article");
    card.className = "secret-card";

    let iconId = "#i-key";
    if (sec.kind === "api_key") iconId = "#i-code";
    else if (sec.kind === "note") iconId = "#i-file";

    const domain = cleanDomain(sec.url);

    card.innerHTML = `
      <div class="secret-card-head">
        <span class="secret-kind"><svg><use href="${iconId}"/></svg> ${sec.kind.toUpperCase()}</span>
        ${domain ? `<span class="secret-domain" title="${escapeHtml(sec.url)}">${escapeHtml(domain)}</span>` : ""}
        <div class="card-tools">
          <button class="icon-btn sm act-copy-val" title="Copy Secret"><svg><use href="#i-copy"/></svg></button>
          <button class="icon-btn sm act-edit" title="Edit"><svg><use href="#i-link"/></svg></button>
          <button class="icon-btn sm danger act-del" title="Delete"><svg><use href="#i-trash"/></svg></button>
        </div>
      </div>
      <div class="secret-card-body">
        <h3 class="secret-title">${escapeHtml(sec.title)}</h3>
        ${sec.username ? `<div class="secret-sub-field"><span class="label">User:</span> <span class="val">${escapeHtml(sec.username)}</span> <button class="btn-copy-inline act-copy-user" title="Copy username">copy</button></div>` : ""}
        <div class="secret-val-row">
          <input type="password" readonly class="secret-val-display" value="${escapeHtml(sec.value || "")}">
          <button class="icon-btn sm act-reveal" title="Show/Hide"><svg><use href="#i-eye"/></svg></button>
        </div>
        ${sec.notes ? `<p class="secret-notes">${escapeHtml(sec.notes)}</p>` : ""}
      </div>
    `;

    // Copy Secret Value
    card.querySelector(".act-copy-val").addEventListener("click", async () => {
      await navigator.clipboard.writeText(sec.value || "");
      toast(`Copied ${sec.title} to clipboard!`);
    });

    // Copy Username
    const copyUserBtn = card.querySelector(".act-copy-user");
    if (copyUserBtn) {
      copyUserBtn.addEventListener("click", async () => {
        await navigator.clipboard.writeText(sec.username || "");
        toast("Username copied to clipboard!");
      });
    }

    // Toggle reveal
    const revealBtn = card.querySelector(".act-reveal");
    const valInput = card.querySelector(".secret-val-display");
    revealBtn.addEventListener("click", () => {
      const use = revealBtn.querySelector("use");
      if (valInput.type === "password") {
        valInput.type = "text";
        use.setAttribute("href", "#i-eye-off");
      } else {
        valInput.type = "password";
        use.setAttribute("href", "#i-eye");
      }
    });

    // Edit
    card.querySelector(".act-edit").addEventListener("click", () => {
      openSecretDrawer(sec);
    });

    // Delete
    card.querySelector(".act-del").addEventListener("click", async () => {
      if (!confirm(`Permanently delete secret "${sec.title}"?`)) return;
      try {
        await window.__clipboardApi(`/api/secrets/${sec.id}`, { method: "DELETE" });
        vaultState.secrets = vaultState.secrets.filter((s) => s.id !== sec.id);
        renderSecretsList(toast);
        toast("Secret deleted");
      } catch (err) {
        toast(err.message, "error");
      }
    });

    container.appendChild(card);
  });
}

function openSecretDrawer(secret) {
  const modal = $("secretDrawerModal");
  if (!modal) return;

  if (secret) {
    vaultState.editingSecretId = secret.id;
    $("secretDrawerTitle").textContent = "Edit Secret";
    $("secretTitle").value = secret.title || "";
    $("secretKind").value = secret.kind || "password";
    $("secretUsername").value = secret.username || "";
    $("secretURL").value = secret.url || "";
    $("secretValue").value = secret.value || "";
    $("secretNotes").value = secret.notes || "";
  } else {
    vaultState.editingSecretId = null;
    $("secretDrawerTitle").textContent = "New Secret";
    $("secretTitle").value = "";
    $("secretKind").value = "password";
    $("secretUsername").value = "";
    $("secretURL").value = "";
    $("secretValue").value = "";
    $("secretNotes").value = "";
  }

  $("secretValue").type = "password";
  $("secretValueToggle")?.querySelector("use")?.setAttribute("href", "#i-eye");
  modal.hidden = false;
}

function closeSecretDrawer() {
  const modal = $("secretDrawerModal");
  if (modal) modal.hidden = true;
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

/* ───────────────────────── Password CSV Import ───────────────────────── */
function wireImportEvents(api, toast) {
  $("importPasswordsBtn")?.addEventListener("click", openImportModal);
  $("importModalClose")?.addEventListener("click", closeImportModal);
  $("cancelImportBtn")?.addEventListener("click", closeImportModal);
  $("importModal")?.addEventListener("click", (e) => {
    if (e.target === $("importModal")) closeImportModal();
  });

  const fileInput = $("csvFileInput");
  const dropZone = $("csvDropZone");

  $("browseCsvBtn")?.addEventListener("click", () => fileInput?.click());
  dropZone?.addEventListener("click", (e) => {
    if (e.target !== $("browseCsvBtn")) fileInput?.click();
  });

  dropZone?.addEventListener("dragover", (e) => {
    e.preventDefault();
    dropZone.classList.add("dragover");
  });
  dropZone?.addEventListener("dragleave", () => dropZone.classList.remove("dragover"));
  dropZone?.addEventListener("drop", (e) => {
    e.preventDefault();
    dropZone.classList.remove("dragover");
    if (e.dataTransfer?.files?.length) {
      handleSelectedCSVFile(e.dataTransfer.files[0], toast);
    }
  });

  fileInput?.addEventListener("change", (e) => {
    if (e.target.files?.length) {
      handleSelectedCSVFile(e.target.files[0], toast);
    }
  });

  $("confirmImportBtn")?.addEventListener("click", async () => {
    if (!vaultState.pendingImport || !vaultState.pendingImport.length) {
      toast("No passwords selected for import", "error");
      return;
    }

    const confirmBtn = $("confirmImportBtn");
    confirmBtn.disabled = true;
    confirmBtn.textContent = "Encrypting & Importing...";

    try {
      const res = await api("/api/secrets/import", {
        method: "POST",
        body: JSON.stringify({ secrets: vaultState.pendingImport }),
      });

      toast(`Successfully imported ${res.imported} passwords into vault!`);
      closeImportModal();

      // Refresh secrets list
      const fresh = await api("/api/secrets");
      vaultState.secrets = fresh.secrets || [];
      renderSecretsList(toast);
    } catch (err) {
      toast(err.message || "Failed to import passwords", "error");
    } finally {
      confirmBtn.disabled = false;
      confirmBtn.innerHTML = `<svg><use href="#i-shield"/></svg> Import &amp; Save Encrypted`;
    }
  });
}

function openImportModal() {
  const modal = $("importModal");
  if (!modal) return;
  modal.hidden = false;
  $("importPreviewArea").hidden = true;
  $("dropzoneTitle").textContent = "Choose or drop passwords.csv";
  $("dropzoneSubtitle").textContent = "Supports Chrome, Firefox, Edge, Safari, and Bitwarden export files";
  if ($("csvFileInput")) $("csvFileInput").value = "";
  vaultState.pendingImport = [];
}

function closeImportModal() {
  const modal = $("importModal");
  if (modal) modal.hidden = true;
  vaultState.pendingImport = [];
}

function handleSelectedCSVFile(file, toast) {
  if (!file.name.endsWith(".csv") && file.type !== "text/csv" && file.type !== "application/vnd.ms-excel") {
    toast("Please select a valid .csv file", "error");
    return;
  }

  const reader = new FileReader();
  reader.onload = (e) => {
    const text = e.target.result;
    const parsed = parseClientCSV(text);
    if (!parsed.length) {
      toast("No credentials found in CSV. Please verify file format.", "error");
      return;
    }

    vaultState.pendingImport = parsed;
    $("dropzoneTitle").textContent = file.name;
    $("dropzoneSubtitle").textContent = `${(file.size / 1024).toFixed(1)} KB • ${parsed.length} accounts found`;

    $("previewCountBadge").textContent = `${parsed.length} Logins Ready`;
    const tbody = $("previewTableBody");
    tbody.innerHTML = "";

    parsed.slice(0, 5).forEach((p) => {
      const tr = document.createElement("tr");
      tr.innerHTML = `
        <td><b>${escapeHtml(p.title || cleanDomain(p.url))}</b></td>
        <td>${escapeHtml(p.username || "—")}</td>
        <td>••••••••</td>
      `;
      tbody.appendChild(tr);
    });

    if (parsed.length > 5) {
      const moreTr = document.createElement("tr");
      moreTr.innerHTML = `<td colspan="3" style="text-align: center; color: var(--ink-3); font-style: italic;">... and ${parsed.length - 5} more logins</td>`;
      tbody.appendChild(moreTr);
    }

    $("importPreviewArea").hidden = false;
  };
  reader.readAsText(file);
}

function parseClientCSV(text) {
  const lines = text.split(/\r?\n/).filter((l) => l.trim().length > 0);
  if (lines.length < 2) return [];

  // Robust line tokenizer supporting quotes
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
