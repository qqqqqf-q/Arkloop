"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const electron_1 = require("electron");
const api = {
    isDesktop: true,
    config: {
        get: () => electron_1.ipcRenderer.invoke('arkloop:config:get'),
        set: (config) => electron_1.ipcRenderer.invoke('arkloop:config:set', config),
        getPath: () => electron_1.ipcRenderer.invoke('arkloop:config:path'),
        onChanged: (callback) => {
            const handler = (_event, config) => callback(config);
            electron_1.ipcRenderer.on('arkloop:config:changed', handler);
            return () => electron_1.ipcRenderer.removeListener('arkloop:config:changed', handler);
        },
    },
    sidecar: {
        getStatus: () => electron_1.ipcRenderer.invoke('arkloop:sidecar:status'),
        restart: () => electron_1.ipcRenderer.invoke('arkloop:sidecar:restart'),
        onStatusChanged: (callback) => {
            const handler = (_event, status) => callback(status);
            electron_1.ipcRenderer.on('arkloop:sidecar:status-changed', handler);
            return () => electron_1.ipcRenderer.removeListener('arkloop:sidecar:status-changed', handler);
        },
    },
    app: {
        getVersion: () => electron_1.ipcRenderer.invoke('arkloop:app:version'),
        quit: () => electron_1.ipcRenderer.invoke('arkloop:app:quit'),
    },
};
electron_1.contextBridge.exposeInMainWorld('arkloop', api);
//# sourceMappingURL=index.js.map