/* ============================================================
   Clipboard Vault — Content Script
   Automatic credential detection, autofill, automatic saving,
   and developer API key & token detection across all platforms
   ============================================================ */

(() => {
  if (window.__clipboardVaultLoaded) return;
  window.__clipboardVaultLoaded = true;

  let pageSecrets = [];
  let pageApiKeys = [];
  let autofilled = false;
  let activeDropdown = null;
  const capturedValues = new Set();

  // ───────────────────────── API Key & Token Signatures ─────────────────────────

  const TOKEN_PATTERNS = [
    { regex: /^ghp_[A-Za-z0-9_]{36,}$/, name: "GitHub Personal Access Token", platform: "github.com", kind: "api_key" },
    { regex: /^github_pat_[A-Za-z0-9_]{80,}$/, name: "GitHub Fine-Grained Token", platform: "github.com", kind: "api_key" },
    { regex: /^gho_[A-Za-z0-9_]{36,}$/, name: "GitHub OAuth Access Token", platform: "github.com", kind: "api_key" },
    { regex: /^ghu_[A-Za-z0-9_]{36,}$/, name: "GitHub User Token", platform: "github.com", kind: "api_key" },
    { regex: /^ghs_[A-Za-z0-9_]{36,}$/, name: "GitHub Server Token", platform: "github.com", kind: "api_key" },
    { regex: /^ghr_[A-Za-z0-9_]{36,}$/, name: "GitHub Refresh Token", platform: "github.com", kind: "api_key" },

    { regex: /^sk-(?:proj-)?[A-Za-z0-9_-]{20,}$/, name: "OpenAI API Key", platform: "openai.com", kind: "api_key" },
    { regex: /^sk-ant-[A-Za-z0-9_-]{20,}$/, name: "Anthropic Claude API Key", platform: "anthropic.com", kind: "api_key" },
    { regex: /^AIzaSy[A-Za-z0-9_-]{33}$/, name: "Google / Gemini API Key", platform: "google.com", kind: "api_key" },
    { regex: /^gsk_[A-Za-z0-9]{40,}$/, name: "Groq Cloud API Key", platform: "groq.com", kind: "api_key" },
    { regex: /^hf_[A-Za-z0-9]{34,}$/, name: "Hugging Face Access Token", platform: "huggingface.co", kind: "api_key" },

    { regex: /^sk_live_[0-9a-zA-Z]{24,}$/, name: "Stripe Live Secret Key", platform: "stripe.com", kind: "api_key" },
    { regex: /^sk_test_[0-9a-zA-Z]{24,}$/, name: "Stripe Test Secret Key", platform: "stripe.com", kind: "api_key" },
    { regex: /^rk_live_[0-9a-zA-Z]{24,}$/, name: "Stripe Restricted Key", platform: "stripe.com", kind: "api_key" },
    { regex: /^pk_live_[0-9a-zA-Z]{24,}$/, name: "Stripe Publishable Key", platform: "stripe.com", kind: "api_key" },

    { regex: /^(?:AKIA|ASIA)[0-9A-Z]{16}$/, name: "AWS Access Key ID", platform: "aws.amazon.com", kind: "api_key" },
    { regex: /^re_[A-Za-z0-9]{24,}$/, name: "Resend API Key", platform: "resend.com", kind: "api_key" },
    { regex: /^SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}$/, name: "SendGrid API Key", platform: "sendgrid.com", kind: "api_key" },
    { regex: /^xox[baprs]-[0-9A-Za-z-]{24,}$/, name: "Slack Token", platform: "slack.com", kind: "api_key" },
  ];

  function identifyToken(val) {
    if (!val || typeof val !== "string") return null;
    const trimmed = val.trim();
    for (const pat of TOKEN_PATTERNS) {
      if (pat.regex.test(trimmed)) {
        return { ...pat, value: trimmed };
      }
    }
    return null;
  }

  // ───────────────────────── Main Initialization ─────────────────────────

  init();

  function init() {
    // 1. Initial queries for stored credentials & API keys
    fetchMatchingCredentials();
    fetchMatchingApiKeys();

    // 2. Watch for dynamic inputs (SPAs, modals, GitHub token creation)
    const observer = new MutationObserver(debounce(() => {
      checkGitHubTokenCreation();
      checkDynamicForms();
    }, 250));

    observer.observe(document.documentElement, { childList: true, subtree: true });

    // 3. GitHub immediate token generator check
    checkGitHubTokenCreation();

    // 4. Form submit & credential capture listeners
    attachFormCaptureListeners();

    // 5. Detect copied API keys / tokens from screen
    attachClipboardCopyListener();

    // 6. Message listener from popup
    chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
      if (message.type === "AUTOFILL") {
        const success = performAutofill(message.username, message.password);
        sendResponse({ success });
        return true;
      }
    });
  }

  // ───────────────────────── GitHub Token Special Detector ─────────────────────────

  function checkGitHubTokenCreation() {
    if (!window.location.hostname.includes("github.com")) return;

    // Classic & fine-grained token display inputs
    const selectors = [
      '#new-oauth-token',
      'input[id*="token"]',
      'clipboard-copy[value^="ghp_"]',
      'clipboard-copy[value^="github_pat_"]',
      '.token-snippet',
      '.new-token',
    ];

    for (const sel of selectors) {
      const el = document.querySelector(sel);
      if (!el) continue;

      const tokenVal = (el.value || el.getAttribute("value") || el.innerText || "").trim();
      const identified = identifyToken(tokenVal);
      if (identified && !capturedValues.has(tokenVal)) {
        capturedValues.add(tokenVal);
        triggerAutoSave(
          "Personal Access Token",
          tokenVal,
          identified.name,
          "github.com",
          identified.kind,
          "Auto-detected on GitHub Settings"
        );
        break;
      }
    }
  }

  // ───────────────────────── Clipboard Copy Detection ─────────────────────────

  function attachClipboardCopyListener() {
    document.addEventListener("copy", () => {
      // Allow the native copy to finish then read selection
      setTimeout(() => {
        try {
          const text = window.getSelection().toString().trim();
          const identified = identifyToken(text);
          if (identified && !capturedValues.has(identified.value)) {
            capturedValues.add(identified.value);
            triggerAutoSave(
              identified.name,
              identified.value,
              identified.name,
              window.location.hostname || identified.platform,
              identified.kind,
              `Copied from ${window.location.hostname}`
            );
          }
        } catch {}
      }, 50);
    });
  }

  // ───────────────────────── Query Logic ─────────────────────────

  function fetchMatchingCredentials() {
    try {
      chrome.runtime.sendMessage(
        {
          type: "QUERY_CREDENTIALS",
          hostname: window.location.hostname,
        },
        (res) => {
          if (chrome.runtime.lastError || !res) return;
          if (res.secrets && res.secrets.length > 0) {
            pageSecrets = res.secrets;
            applyAutofill();
            startAutofillRetry();
          }
        }
      );
    } catch {}
  }

  function startAutofillRetry() {
    let attempts = 0;
    const interval = setInterval(() => {
      attempts++;
      if (autofilled || attempts > 15) {
        clearInterval(interval);
        return;
      }
      applyAutofill();
    }, 350);
  }

  function fetchMatchingApiKeys() {
    try {
      chrome.runtime.sendMessage(
        {
          type: "QUERY_API_KEYS",
          hostname: window.location.hostname,
        },
        (res) => {
          if (chrome.runtime.lastError || !res) return;
          if (res.apiKeys && res.apiKeys.length > 0) {
            pageApiKeys = res.apiKeys;
            attachApiKeyInputListeners();
          }
        }
      );
    } catch {}
  }

  function checkDynamicForms() {
    if (!autofilled && pageSecrets.length > 0) {
      applyAutofill();
    } else if (pageSecrets.length === 0) {
      fetchMatchingCredentials();
    }
    attachApiKeyInputListeners();
  }

  // ───────────────────────── Autofill Password ─────────────────────────

  function applyAutofill() {
    if (!pageSecrets || pageSecrets.length === 0) return;

    // Pick primary credential (first matching)
    const sec = pageSecrets[0];
    let filled = false;

    const passwordInputs = Array.from(document.querySelectorAll('input[type="password"]')).filter((el) => {
      return isFieldVisible(el) || el.offsetWidth > 0 || el.offsetHeight > 0;
    });

    if (passwordInputs.length > 0) {
      const targetPassword = passwordInputs[0];
      if (sec.value && targetPassword.value !== sec.value) {
        setInputValue(targetPassword, sec.value);
        filled = true;
      }

      if (sec.username) {
        const usernameInput = findUsernameInput(targetPassword);
        if (usernameInput && usernameInput.value !== sec.username) {
          setInputValue(usernameInput, sec.username);
          filled = true;
        }
      }
    } else {
      // Multi-step login flow (step 1: username/email only, no password input mounted yet)
      if (sec.username) {
        const standaloneUser = findStandaloneUsernameInput();
        if (standaloneUser && !standaloneUser.value) {
          setInputValue(standaloneUser, sec.username);
          filled = true;
        }
      }
    }

    if (filled) {
      autofilled = true;
    }

    // If multiple credentials exist, attach picker dropdown to easily switch
    if (pageSecrets.length > 1) {
      attachCredentialPicker(pageSecrets);
    }
  }

  function performAutofill(username, password) {
    let filled = false;
    const passwordInputs = Array.from(document.querySelectorAll('input[type="password"]')).filter((el) => {
      return isFieldVisible(el) || el.offsetWidth > 0 || el.offsetHeight > 0;
    });

    if (passwordInputs.length > 0) {
      const targetPassword = passwordInputs[0];
      if (password) {
        setInputValue(targetPassword, password);
        filled = true;
      }
      if (username) {
        const usernameInput = findUsernameInput(targetPassword);
        if (usernameInput) {
          setInputValue(usernameInput, username);
          filled = true;
        }
      }
    } else if (username) {
      const standaloneUser = findStandaloneUsernameInput();
      if (standaloneUser) {
        setInputValue(standaloneUser, username);
        filled = true;
      }
    }
    return filled;
  }

  function findUsernameInput(targetPassword) {
    const form = targetPassword ? targetPassword.closest("form") : null;
    const scope = form || document;

    const candidate =
      scope.querySelector('input[autocomplete="username"]') ||
      scope.querySelector('input[autocomplete="email"]') ||
      scope.querySelector('input[type="email"]') ||
      scope.querySelector('input[name*="user" i], input[name*="login" i], input[name*="email" i], input[id*="user" i], input[id*="login" i], input[id*="email" i]') ||
      scope.querySelector('input[type="text"]');

    if (candidate && candidate !== targetPassword && (isFieldVisible(candidate) || candidate.offsetWidth > 0)) {
      return candidate;
    }
    return null;
  }

  function findStandaloneUsernameInput() {
    const selectors = [
      'input[autocomplete="username"]',
      'input[autocomplete="email"]',
      'input[type="email"]',
      'input[name*="user" i]',
      'input[name*="login" i]',
      'input[name*="email" i]',
      'input[id*="user" i]',
      'input[id*="login" i]',
      'input[id*="email" i]',
      'form input[type="text"]',
    ];
    for (const sel of selectors) {
      const el = document.querySelector(sel);
      if (el && (isFieldVisible(el) || el.offsetWidth > 0)) {
        return el;
      }
    }
    return null;
  }

  function setInputValue(input, val) {
    if (!input || val === undefined || val === null) return;
    try {
      input.focus();
      // Framework synthetic setter override for React/Vue/Angular
      const proto = Object.getPrototypeOf(input);
      const desc = Object.getOwnPropertyDescriptor(proto, "value") ||
                   Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value");
      if (desc && desc.set) {
        desc.set.call(input, val);
      } else {
        input.value = val;
      }
      input.dispatchEvent(new Event("input", { bubbles: true, cancelable: true }));
      input.dispatchEvent(new Event("change", { bubbles: true, cancelable: true }));
      input.blur();
    } catch {
      input.value = val;
    }
  }

  function isFieldVisible(el) {
    if (!el) return false;
    const style = window.getComputedStyle(el);
    return (
      style.display !== "none" &&
      style.visibility !== "hidden" &&
      style.opacity !== "0" &&
      el.offsetWidth > 0 &&
      el.offsetHeight > 0
    );
  }

  // ───────────────────────── API Key Autofill & Detection ─────────────────────────

  function isApiKeyField(el) {
    if (!el || !isFieldVisible(el)) return false;
    if (el.tagName !== "INPUT" && el.tagName !== "TEXTAREA") return false;
    const meta = `${el.id || ""} ${el.name || ""} ${el.placeholder || ""} ${el.getAttribute("aria-label") || ""}`.toLowerCase();
    return /api[_-]?key|access[_-]?token|secret[_-]?key|auth[_-]?token|bearer|pat[_-]?token|personal[_-]?access|openai[_-]?key|github[_-]?token/i.test(meta);
  }

  function attachApiKeyInputListeners() {
    const inputs = Array.from(document.querySelectorAll('input, textarea')).filter(isApiKeyField);
    inputs.forEach((input) => {
      if (input.dataset.cbVaultApiKeyBound) return;
      input.dataset.cbVaultApiKeyBound = "true";

      // Autofill picker on focus if we have saved API keys
      input.addEventListener("focus", () => {
        if (pageApiKeys.length > 0) {
          showApiKeyPicker(input, pageApiKeys);
        }
      });

      // Auto-save on blur / input change if valid API key typed
      input.addEventListener("change", () => {
        const val = input.value.trim();
        const identified = identifyToken(val);
        if (identified && !capturedValues.has(val)) {
          capturedValues.add(val);
          triggerAutoSave(
            identified.name,
            val,
            identified.name,
            window.location.hostname,
            "api_key"
          );
        }
      });
    });
  }

  function showApiKeyPicker(targetInput, keys) {
    if (activeDropdown) activeDropdown.remove();

    const rect = targetInput.getBoundingClientRect();
    const dropdown = document.createElement("div");
    dropdown.id = "clipboard-vault-api-picker";
    dropdown.style.cssText = `
      position: fixed;
      top: ${rect.bottom + window.scrollY + 4}px;
      left: ${rect.left + window.scrollX}px;
      width: ${Math.max(rect.width, 260)}px;
      background: #121418;
      border: 1px solid #2e3340;
      border-radius: 8px;
      box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
      z-index: 2147483647;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      font-size: 12.5px;
      color: #f4f5f8;
      padding: 6px;
      box-sizing: border-box;
    `;

    const header = document.createElement("div");
    header.style.cssText = `
      font-size: 10px;
      font-weight: 700;
      letter-spacing: 0.05em;
      text-transform: uppercase;
      color: #8b92a5;
      padding: 4px 8px 6px;
      border-bottom: 1px solid #232733;
      margin-bottom: 4px;
      display: flex;
      justify-content: space-between;
    `;
    const headerTitle = document.createElement("span");
    headerTitle.textContent = "🔑 Clipboard Vault";
    const headerSub = document.createElement("span");
    headerSub.textContent = "Saved API Keys";
    header.appendChild(headerTitle);
    header.appendChild(headerSub);
    dropdown.appendChild(header);

    keys.forEach((key) => {
      const item = document.createElement("div");
      item.style.cssText = `
        padding: 8px 10px;
        border-radius: 6px;
        cursor: pointer;
        display: flex;
        flex-direction: column;
        gap: 2px;
        transition: background 0.1s;
      `;
      const masked = key.value ? `${key.value.slice(0, 7)}••••••••${key.value.slice(-4)}` : "••••••••";
      const keyTitle = document.createElement("b");
      keyTitle.style.cssText = "font-size: 12px; color: #ffffff;";
      keyTitle.textContent = key.title;

      const keyVal = document.createElement("span");
      keyVal.style.cssText = "font-size: 10.5px; color: #8b92a5; font-family: ui-monospace, monospace;";
      keyVal.textContent = masked;

      item.appendChild(keyTitle);
      item.appendChild(keyVal);
      item.addEventListener("mouseenter", () => { item.style.background = "#232733"; });
      item.addEventListener("mouseleave", () => { item.style.background = "transparent"; });
      item.addEventListener("mousedown", (e) => {
        e.preventDefault();
        setInputValue(targetInput, key.value);
        dropdown.remove();
        activeDropdown = null;
      });
      dropdown.appendChild(item);
    });

    document.body.appendChild(dropdown);
    activeDropdown = dropdown;
  }

  // ───────────────────────── Multi-Account Login Picker ─────────────────────────

  function attachCredentialPicker(secrets) {
    const passwordInput = document.querySelector('input[type="password"]');
    if (!passwordInput) return;
    const usernameInput = findUsernameInput(passwordInput) || passwordInput;

    const showPicker = () => {
      if (activeDropdown) activeDropdown.remove();

      const rect = usernameInput.getBoundingClientRect();
      const dropdown = document.createElement("div");
      dropdown.id = "clipboard-vault-picker";
      dropdown.style.cssText = `
        position: fixed;
        top: ${rect.bottom + window.scrollY + 4}px;
        left: ${rect.left + window.scrollX}px;
        width: ${Math.max(rect.width, 240)}px;
        background: #121418;
        border: 1px solid #2e3340;
        border-radius: 8px;
        box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
        z-index: 2147483647;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
        font-size: 12.5px;
        color: #f4f5f8;
        padding: 6px;
        box-sizing: border-box;
      `;

      const header = document.createElement("div");
      header.style.cssText = `
        font-size: 10px;
        font-weight: 700;
        letter-spacing: 0.05em;
        text-transform: uppercase;
        color: #8b92a5;
        padding: 4px 8px 6px;
        border-bottom: 1px solid #232733;
        margin-bottom: 4px;
        display: flex;
        justify-content: space-between;
      `;
      const headerTitle = document.createElement("span");
      headerTitle.textContent = "Clipboard Vault";
      const headerCount = document.createElement("span");
      headerCount.textContent = `${secrets.length} Accounts`;
      header.appendChild(headerTitle);
      header.appendChild(headerCount);
      dropdown.appendChild(header);

      secrets.forEach((sec) => {
        const item = document.createElement("div");
        item.style.cssText = `
          padding: 8px 10px;
          border-radius: 6px;
          cursor: pointer;
          display: flex;
          flex-direction: column;
          gap: 2px;
          transition: background 0.1s;
        `;
        const credUser = document.createElement("b");
        credUser.style.cssText = "font-size: 12px; color: #ffffff;";
        credUser.textContent = sec.username || sec.title;

        const credTitle = document.createElement("span");
        credTitle.style.cssText = "font-size: 10.5px; color: #8b92a5;";
        credTitle.textContent = sec.title || "Saved Password";

        item.appendChild(credUser);
        item.appendChild(credTitle);
        item.addEventListener("mouseenter", () => { item.style.background = "#232733"; });
        item.addEventListener("mouseleave", () => { item.style.background = "transparent"; });
        item.addEventListener("mousedown", (e) => {
          e.preventDefault();
          performAutofill(sec.username, sec.value);
          dropdown.remove();
          activeDropdown = null;
        });
        dropdown.appendChild(item);
      });

      document.body.appendChild(dropdown);
      activeDropdown = dropdown;
    };

    usernameInput.addEventListener("focus", showPicker);
    document.addEventListener("click", (e) => {
      if (activeDropdown && !activeDropdown.contains(e.target) && e.target !== usernameInput) {
        activeDropdown.remove();
        activeDropdown = null;
      }
    });
  }

  // ───────────────────────── Form Capture Listeners ─────────────────────────

  function attachFormCaptureListeners() {
    document.addEventListener("submit", handleFormSubmit, true);

    document.addEventListener("keydown", (e) => {
      if (e.key === "Enter") {
        if (e.target && e.target.matches('input[type="password"]')) {
          const form = e.target.closest("form");
          if (form) handleFormSubmit({ target: form });
          else captureStandaloneInputs(e.target);
        } else if (e.target && isApiKeyField(e.target)) {
          captureApiKeyInput(e.target);
        }
      }
    }, true);

    document.addEventListener("click", (e) => {
      const btn = e.target.closest('button, input[type="submit"], a[role="button"]');
      if (!btn) return;

      const text = (btn.innerText || btn.value || "").toLowerCase();
      if (/log\s*in|sign\s*in|submit|continue|next|authorize|save\s*key|create\s*token/i.test(text)) {
        const form = btn.closest("form");
        if (form) {
          handleFormSubmit({ target: form });
        } else {
          const pass = document.querySelector('input[type="password"]');
          if (pass && pass.value) captureStandaloneInputs(pass);
          const apiKey = Array.from(document.querySelectorAll('input, textarea')).find(isApiKeyField);
          if (apiKey && apiKey.value) captureApiKeyInput(apiKey);
        }
      }
    }, true);
  }

  function handleFormSubmit(e) {
    const form = e.target;
    if (!form || !(form instanceof HTMLElement)) return;

    // Check for API key field first
    const apiKeyField = Array.from(form.querySelectorAll('input, textarea')).find(isApiKeyField);
    if (apiKeyField && apiKeyField.value) {
      captureApiKeyInput(apiKeyField);
      return;
    }

    // Otherwise standard password form
    const passwordInput = form.querySelector('input[type="password"]');
    if (!passwordInput || !passwordInput.value) return;

    const usernameInput = findUsernameInput(passwordInput);
    const username = usernameInput ? usernameInput.value : "";
    const password = passwordInput.value;

    triggerAutoSave(username, password);
  }

  function captureStandaloneInputs(passwordInput) {
    if (!passwordInput || !passwordInput.value) return;
    const usernameInput = findUsernameInput(passwordInput);
    const username = usernameInput ? usernameInput.value : "";
    const password = passwordInput.value;
    triggerAutoSave(username, password);
  }

  function captureApiKeyInput(input) {
    const val = input.value.trim();
    if (!val || capturedValues.has(val)) return;
    capturedValues.add(val);

    const identified = identifyToken(val);
    const title = identified ? identified.name : `${window.location.hostname} API Key`;
    const platform = identified ? identified.platform : window.location.hostname;

    triggerAutoSave(title, val, title, platform, "api_key");
  }

  function triggerAutoSave(username, value, title, platform, kind = "password", notes = "") {
    if (!value) return;

    const hostname = platform || window.location.hostname;
    const finalTitle = title || (hostname ? `${hostname.charAt(0).toUpperCase() + hostname.slice(1)} ${kind === "api_key" ? "API Key" : "Account"}` : "Saved Secret");

    try {
      chrome.runtime.sendMessage(
        {
          type: "AUTO_SAVE_CREDENTIAL",
          url: window.location.href,
          hostname: hostname,
          username: username,
          password: value,
          value: value,
          title: finalTitle,
          kind: kind,
          notes: notes,
        },
        (res) => {
          if (chrome.runtime.lastError || !res) return;

          if (res.success) {
            const icon = kind === "api_key" ? "🔑" : "🔒";
            if (res.status === "created") {
              showInPageNotification(`${icon} Clipboard Vault: Saved ${finalTitle}`);
            } else if (res.status === "updated") {
              showInPageNotification(`${icon} Clipboard Vault: Updated ${finalTitle}`);
            }
          }
        }
      );
    } catch {}
  }

  // ───────────────────────── In-Page Notification ─────────────────────────

  function showInPageNotification(message) {
    const existing = document.getElementById("clipboard-vault-toast");
    if (existing) existing.remove();

    const toast = document.createElement("div");
    toast.id = "clipboard-vault-toast";
    toast.style.cssText = `
      position: fixed;
      top: 18px;
      right: 18px;
      background: #09090b;
      color: #f4f4f5;
      border: 1px solid #27272a;
      border-radius: 8px;
      padding: 10px 16px;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      font-size: 13px;
      font-weight: 500;
      box-shadow: 0 10px 25px rgba(0, 0, 0, 0.4);
      z-index: 2147483647;
      display: flex;
      align-items: center;
      gap: 10px;
      animation: cbSlideIn 0.25s cubic-bezier(0.16, 1, 0.3, 1);
      transition: opacity 0.2s, transform 0.2s;
    `;

    const dot = document.createElement("span");
    dot.style.cssText = "display:inline-block; width:8px; height:8px; border-radius:50%; background:#10b981;";
    const msgText = document.createElement("span");
    msgText.textContent = message;
    toast.appendChild(dot);
    toast.appendChild(msgText);

    document.body.appendChild(toast);

    setTimeout(() => {
      toast.style.opacity = "0";
      toast.style.transform = "translateY(-6px)";
      setTimeout(() => toast.remove(), 250);
    }, 3500);
  }

  function debounce(func, wait) {
    let timeout;
    return function executedFunction(...args) {
      const later = () => {
        clearTimeout(timeout);
        func(...args);
      };
      clearTimeout(timeout);
      timeout = setTimeout(later, wait);
    };
  }

  function escapeHtml(str) {
    return (str || "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#039;");
  }
})();
