// A lightweight heartbeat from ordinary Edge tabs wakes the Manifest V3
// service worker after the browser has suspended it. The worker itself owns
// all broker and debugger activity; content scripts never receive commands.
const wakeBroker = () => {
  try {
    chrome.runtime.sendMessage({ type: 'brokerWake' }).catch(() => {});
  } catch {
    // Extension reloads can invalidate an already-injected content script.
  }
};

wakeBroker();
setInterval(wakeBroker, 10000);
