const statusElement = document.querySelector('#status');

function setStatus(message) {
  statusElement.textContent = message;
}

function isLoopbackRelay(url) {
  return (url.protocol === 'ws:' || url.protocol === 'wss:') &&
    (url.hostname === '127.0.0.1' || url.hostname === 'localhost' || url.hostname === '[::1]');
}

async function connect() {
  const query = new URLSearchParams(location.search);
  const relayValue = query.get('mcpRelayUrl');
  const protocolVersion = Number(query.get('protocolVersion') || '2');

  if (!relayValue)
    throw new Error('Missing local MCP relay URL.');

  const relayUrl = new URL(relayValue);
  if (!isLoopbackRelay(relayUrl))
    throw new Error('Refusing a non-loopback MCP relay.');
  if (protocolVersion !== 2)
    throw new Error(`Unsupported extension protocol version: ${protocolVersion}`);

  const response = await chrome.runtime.sendMessage({
    type: 'connectAllTabs',
    relayUrl: relayUrl.toString(),
    protocolVersion,
  });
  if (!response?.success)
    throw new Error(response?.error || 'The extension background worker rejected the connection.');

  setStatus(`Connected ${response.tabCount} tab${response.tabCount === 1 ? '' : 's'}. This temporary tab will close.`);
  setTimeout(() => window.close(), 300);
}

connect().catch(error => {
  setStatus(`Connection failed: ${error.message}`);
});
