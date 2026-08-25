const ALLOWED_COMMANDS = new Set([
  'chrome.debugger.attach',
  'chrome.debugger.detach',
  'chrome.debugger.sendCommand',
  'chrome.tabs.create',
  'chrome.tabs.remove',
]);

const NON_DEBUGGABLE_SCHEMES = [
  'chrome:',
  'edge:',
  'devtools:',
  'chrome-extension:',
  'extension:',
];

let activeConnection;
const BROKER_ORIGIN = 'http://127.0.0.1:55712';
const BROKER_ALARM = 'edge-mcp-broker-poll';
let brokerCursor = 0;
let brokerPollRunning = false;

function isDebuggableTab(tab) {
  return tab?.id !== undefined &&
    !!tab.url &&
    !NON_DEBUGGABLE_SCHEMES.some(scheme => tab.url.startsWith(scheme));
}

function resolveChromeMethod(fullMethod) {
  const parts = fullMethod.split('.');
  if (parts[0] !== 'chrome' || parts.length < 3)
    throw new Error(`Invalid Chrome method: ${fullMethod}`);

  let owner = chrome;
  for (let index = 1; index < parts.length - 1; index++) {
    owner = owner?.[parts[index]];
    if (!owner)
      throw new Error(`Unknown Chrome API path for ${fullMethod}`);
  }

  const method = owner[parts.at(-1)];
  if (typeof method !== 'function')
    throw new Error(`Chrome API member is not callable: ${fullMethod}`);
  return { owner, method };
}

async function invokeChromeMethod(fullMethod, args) {
  const { owner, method } = resolveChromeMethod(fullMethod);
  return await method.apply(owner, args);
}

class AllTabsConnection {
  constructor(relayUrl) {
    this._relayUrl = relayUrl;
    this._socket = undefined;
    this._attachedTabIds = new Set();
    this._advertisedTabIds = new Set();
    this._pendingCreatedTabs = new Map();
    this._createCommandDepth = 0;
    this._keepaliveTimer = undefined;
    this._closed = false;
  }

  async open() {
    const socket = new WebSocket(this._relayUrl);
    this._socket = socket;
    socket.onmessage = event => void this._onMessage(event);
    socket.onclose = () => void this.close();

    await new Promise((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error('Local MCP relay connection timed out.')), 5000);
      socket.onopen = () => {
        clearTimeout(timeout);
        resolve();
      };
      socket.onerror = () => {
        clearTimeout(timeout);
        reject(new Error('Could not connect to the local MCP relay.'));
      };
    });

    const tabs = await chrome.tabs.query({});
    for (const tab of tabs)
      this.advertiseTab(tab);
    this._send({ method: 'extension.initialized', params: [] });

    // WebSocket traffic keeps a Manifest V3 service worker alive on current Edge.
    this._keepaliveTimer = setInterval(() => {
      this._send({ method: 'extension.keepalive', params: [] });
    }, 20000);

    return this._advertisedTabIds.size;
  }

  advertiseTab(tab) {
    if (!isDebuggableTab(tab) || this._advertisedTabIds.has(tab.id))
      return;
    if (this._createCommandDepth) {
      this._pendingCreatedTabs.set(tab.id, tab);
      return;
    }
    this._advertisedTabIds.add(tab.id);
    this._send({ method: 'chrome.tabs.onCreated', params: [tab] });
  }

  reconcileTab(tabId, tab) {
    if (isDebuggableTab(tab)) {
      this.advertiseTab(tab);
      return;
    }
    this.removeTab(tabId);
  }

  removeTab(tabId) {
    this._pendingCreatedTabs.delete(tabId);
    if (!this._advertisedTabIds.delete(tabId))
      return;
    this._send({ method: 'chrome.tabs.onRemoved', params: [tabId] });
  }

  forwardDebuggerEvent(source, method, params) {
    if (!this._attachedTabIds.has(source.tabId))
      return;
    this._send({ method: 'chrome.debugger.onEvent', params: [source, method, params] });
  }

  forwardDebuggerDetach(source, reason) {
    if (!this._attachedTabIds.delete(source.tabId))
      return;
    this._send({ method: 'chrome.debugger.onDetach', params: [source, reason] });
  }

  async close(reason = 'MCP relay closed') {
    if (this._closed)
      return;
    this._closed = true;
    if (this._keepaliveTimer)
      clearInterval(this._keepaliveTimer);
    if (activeConnection === this)
      activeConnection = undefined;

    const attachedTabIds = [...this._attachedTabIds];
    this._attachedTabIds.clear();
    await Promise.all(attachedTabIds.map(tabId => chrome.debugger.detach({ tabId }).catch(() => {})));

    if (this._socket?.readyState === WebSocket.OPEN)
      this._socket.close(1000, reason);
  }

  async _onMessage(event) {
    let message;
    try {
      message = JSON.parse(event.data);
    } catch (error) {
      this._send({ error: `Invalid relay message: ${error.message}` });
      return;
    }

    const response = { id: message.id };
    try {
      if (!ALLOWED_COMMANDS.has(message.method))
        throw new Error(`Unsupported relay command: ${message.method}`);
      const args = message.params ?? [];
      if (message.method === 'chrome.tabs.create') {
        this._createCommandDepth++;
        try {
          response.result = await invokeChromeMethod(message.method, args) ?? {};
          // BrowserModel.createTarget owns attachment for the tab returned by
          // this command. Suppress the concurrent onCreated event so the
          // debugger is not attached twice; unrelated user-created tabs are
          // flushed through the normal event path immediately afterwards.
          if (response.result.id !== undefined) {
            this._pendingCreatedTabs.delete(response.result.id);
            this._advertisedTabIds.add(response.result.id);
          }
        } finally {
          this._createCommandDepth--;
          if (!this._createCommandDepth) {
            const pendingTabs = [...this._pendingCreatedTabs.values()];
            this._pendingCreatedTabs.clear();
            for (const tab of pendingTabs)
              this.advertiseTab(tab);
          }
        }
      } else {
        response.result = await invokeChromeMethod(message.method, args) ?? {};
      }

      const tabId = args[0]?.tabId;
      if (message.method === 'chrome.debugger.attach' && tabId !== undefined)
        this._attachedTabIds.add(tabId);
      if (message.method === 'chrome.debugger.detach' && tabId !== undefined)
        this._attachedTabIds.delete(tabId);
    } catch (error) {
      response.error = error.message;
    }
    this._send(response);
  }

  _send(message) {
    if (this._socket?.readyState === WebSocket.OPEN)
      this._socket.send(JSON.stringify(message));
  }
}

async function replaceActiveConnection(relayUrl) {
  let connection;
  if (activeConnection)
    await activeConnection.close('A replacement Edge MCP client connected');
  connection = new AllTabsConnection(relayUrl);
  try {
    activeConnection = connection;
    const tabCount = await connection.open();
    return tabCount;
  } catch (error) {
    await connection?.close('Edge MCP connection failed');
    throw error;
  }
}

chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  if (message?.type === 'brokerWake') {
    void pollBroker();
    return false;
  }

  if (message?.type !== 'connectAllTabs')
    return false;

  void replaceActiveConnection(message.relayUrl).then(
    tabCount => sendResponse({ success: true, tabCount }),
    error => sendResponse({ success: false, error: error.message }),
  );
  return true;
});

const delay = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));

async function pollBroker() {
  if (brokerPollRunning)
    return;
  brokerPollRunning = true;
  try {
    while (true) {
      try {
        const response = await fetch(`${BROKER_ORIGIN}/poll?cursor=${brokerCursor}`, {
          cache: 'no-store',
        });
        if (!response.ok)
          throw new Error(`broker poll failed: ${response.status}`);
        const payload = await response.json();
        if (!payload.command)
          continue;
        const { cursor, relayUrl, protocolVersion } = payload.command;
        if (protocolVersion !== 2)
          throw new Error(`unsupported broker protocol version: ${protocolVersion}`);
        await replaceActiveConnection(relayUrl);
        const acknowledgement = await fetch(`${BROKER_ORIGIN}/ack`, {
          method: 'POST',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ cursor }),
        });
        if (!acknowledgement.ok)
          throw new Error(`broker acknowledgement failed: ${acknowledgement.status}`);
        brokerCursor = cursor;
      } catch {
        await delay(1000);
      }
    }
  } finally {
    brokerPollRunning = false;
  }
}

chrome.alarms.create(BROKER_ALARM, { periodInMinutes: 0.5 });
chrome.alarms.onAlarm.addListener(alarm => {
  if (alarm.name === BROKER_ALARM)
    void pollBroker();
});
chrome.tabs.onCreated.addListener(() => void pollBroker());
chrome.tabs.onUpdated.addListener(() => void pollBroker());
void pollBroker();

chrome.debugger.onEvent.addListener((source, method, params) => {
  activeConnection?.forwardDebuggerEvent(source, method, params);
});

chrome.debugger.onDetach.addListener((source, reason) => {
  activeConnection?.forwardDebuggerDetach(source, reason);
});

chrome.tabs.onCreated.addListener(tab => {
  activeConnection?.advertiseTab(tab);
});

chrome.tabs.onUpdated.addListener((tabId, _changeInfo, tab) => {
  activeConnection?.reconcileTab(tabId, tab);
});

chrome.tabs.onRemoved.addListener(tabId => {
  activeConnection?.removeTab(tabId);
});
