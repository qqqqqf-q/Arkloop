"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.registerIpcHandlers = registerIpcHandlers;
const electron_1 = require("electron");
const config_1 = require("./config");
const sidecar_1 = require("./sidecar");
function registerIpcHandlers(getWindow) {
    electron_1.ipcMain.handle('arkloop:config:get', () => {
        return (0, config_1.loadConfig)();
    });
    electron_1.ipcMain.handle('arkloop:config:set', async (_event, config) => {
        const prev = (0, config_1.loadConfig)();
        (0, config_1.saveConfig)(config);
        // 模式切换时重启 sidecar
        if (prev.mode !== config.mode || prev.local.port !== config.local.port) {
            await (0, sidecar_1.stopSidecar)();
            if (config.mode === 'local') {
                await (0, sidecar_1.startSidecar)(config.local.port);
            }
            // 通知渲染进程重新加载
            const win = getWindow();
            if (win)
                win.webContents.send('arkloop:config:changed', config);
        }
        return { ok: true };
    });
    electron_1.ipcMain.handle('arkloop:config:path', () => {
        return (0, config_1.getConfigPath)();
    });
    electron_1.ipcMain.handle('arkloop:sidecar:status', () => {
        return (0, sidecar_1.getSidecarStatus)();
    });
    electron_1.ipcMain.handle('arkloop:sidecar:restart', async () => {
        const config = (0, config_1.loadConfig)();
        await (0, sidecar_1.stopSidecar)();
        await (0, sidecar_1.startSidecar)(config.local.port);
        return (0, sidecar_1.getSidecarStatus)();
    });
    electron_1.ipcMain.handle('arkloop:app:version', () => {
        const { app } = require('electron');
        return app.getVersion();
    });
    electron_1.ipcMain.handle('arkloop:app:quit', () => {
        const { app } = require('electron');
        app.quit();
    });
}
//# sourceMappingURL=ipc.js.map