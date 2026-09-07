/* ============================================================
   Clipboard Vault — Background Service Worker
   Cross-browser WebExtension standard (Chrome & Firefox)
   ============================================================ */

chrome.runtime.onInstalled.addListener(() => {
  // Initialize default server URL if not already set
  chrome.storage.local.get(["serverUrl"], (res) => {
    if (!res.serverUrl) {
      chrome.storage.local.set({ serverUrl: "http://localhost:8080" });
    }
  });
});

// Listen for messages from content scripts or popup
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.type === "GET_ACTIVE_TAB") {
    chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
      sendResponse({ tab: tabs && tabs[0] ? tabs[0] : null });
    });
    return true; // async response
  }

  if (message.type === "CREDENTIAL_SUBMITTED") {
    // Store detected credential for quick-save offer
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
