/**
 * Base content script — injected into all pages managed by PatAI.
 * Handles generic actions that work on any site.
 *
 * Site-specific feed extraction is in feeds/*.js — those scripts register
 * themselves via window._patai_feed so the base handler can delegate.
 *
 * Actions:
 *   - get_html      → return full page HTML, URL, title
 *   - scroll         → scroll the page by a given amount
 *   - get_feed       → delegate to site-specific feed extractor
 *   - get_feed_html  → delegate to site-specific feed extractor
 */

// Registry for site-specific feed extractors.
// Feed scripts (feeds/linkedin.js, feeds/twitter.js) register here.
window._patai_feed = window._patai_feed || {
  extractUrls: null,
  extractHtml: null,
};

// Keep the background service worker alive by holding a port open.
// Chrome kills the SW after 30s of inactivity; an open port prevents that.
(function keepAlive() {
  try {
    const port = chrome.runtime.connect({ name: "keepalive" });
    port.onDisconnect.addListener(() => {
      // Port auto-closes after ~5 min; reopen it
      setTimeout(keepAlive, 1000);
    });
  } catch (e) {}
})();


// ---------------------------------------------------------------------------
// Message handler
// ---------------------------------------------------------------------------

chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  const { action } = msg;

  switch (action) {
    case "get_html":
      sendResponse({
        html: document.documentElement.outerHTML,
        url: window.location.href,
        title: document.title,
      });
      break;

    case "scroll":
      window.scrollBy(0, msg.amount || 1000);
      sendResponse({ scrollY: window.scrollY });
      break;

    case "get_feed": {
      const extractor = window._patai_feed.extractUrls;
      if (extractor) {
        sendResponse({ posts: extractor() });
      } else {
        // Fallback: extract all links from the page
        sendResponse({ posts: extractAllLinks() });
      }
      break;
    }

    case "get_feed_html": {
      const extractor = window._patai_feed.extractHtml;
      if (extractor) {
        sendResponse({ posts: extractor() });
      } else {
        sendResponse({ posts: [], error: "No feed extractor for this site" });
      }
      break;
    }

    default:
      sendResponse({ error: `Unknown action: ${action}` });
  }

  return true;
});

// ---------------------------------------------------------------------------
// Generic link extraction fallback
// ---------------------------------------------------------------------------

/**
 * Extract all unique links from the page as a simple fallback
 * when no site-specific feed extractor is available.
 */
function extractAllLinks() {
  const urls = new Set();
  for (const a of document.querySelectorAll("a[href]")) {
    const href = a.getAttribute("href") || "";
    if (!href || href.startsWith("#") || href.startsWith("javascript:")) continue;
    try {
      const url = new URL(href, window.location.origin);
      if (url.protocol.startsWith("http")) {
        urls.add(url.href.split("#")[0]);
      }
    } catch (e) {
      // skip invalid URLs
    }
  }
  return Array.from(urls);
}
