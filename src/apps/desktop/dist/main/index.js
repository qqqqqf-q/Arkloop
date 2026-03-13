"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
const electron_1 = require("electron");
const path = __importStar(require("path"));
const config_1 = require("./config");
const sidecar_1 = require("./sidecar");
const tray_1 = require("./tray");
const ipc_1 = require("./ipc");
let mainWindow = null;
function getWindow() {
    return mainWindow;
}
function createWindow() {
    const config = (0, config_1.loadConfig)();
    const win = new electron_1.BrowserWindow({
        width: config.window.width,
        height: config.window.height,
        minWidth: 900,
        minHeight: 600,
        title: 'Arkloop',
        show: false,
        webPreferences: {
            preload: path.join(__dirname, '..', 'preload', 'index.js'),
            contextIsolation: true,
            nodeIntegration: false,
            sandbox: true,
        },
        titleBarStyle: process.platform === 'darwin' ? 'hiddenInset' : 'default',
        trafficLightPosition: { x: 12, y: 12 },
    });
    // 窗口大小变化时持久化
    win.on('resize', () => {
        if (win.isMaximized())
            return;
        const [width, height] = win.getSize();
        const cfg = (0, config_1.loadConfig)();
        cfg.window = { width, height };
        (0, config_1.saveConfig)(cfg);
    });
    // 关闭时最小化到托盘而非退出
    win.on('close', (e) => {
        if (!isQuitting) {
            e.preventDefault();
            win.hide();
        }
    });
    win.once('ready-to-show', () => {
        win.show();
    });
    return win;
}
function loadContent(win) {
    const config = (0, config_1.loadConfig)();
    if (process.env.ELECTRON_DEV === 'true') {
        // 开发模式: 加载 Vite dev server
        const devUrl = process.env.VITE_DEV_URL || 'http://localhost:5173';
        win.loadURL(devUrl);
        win.webContents.openDevTools({ mode: 'detach' });
    }
    else if (electron_1.app.isPackaged) {
        // 生产打包模式
        const rendererPath = path.join(process.resourcesPath, 'renderer', 'index.html');
        win.loadFile(rendererPath);
    }
    else {
        // 开发模式但非 ELECTRON_DEV（直接 build 后测试）
        const webDist = path.resolve(__dirname, '..', '..', '..', 'web', 'dist', 'index.html');
        win.loadFile(webDist);
    }
    // 注入连接配置
    win.webContents.on('did-finish-load', () => {
        const apiBaseUrl = config.mode === 'local'
            ? `http://127.0.0.1:${config.local.port}`
            : config.mode === 'saas'
                ? config.saas.baseUrl
                : config.selfHosted.baseUrl;
        win.webContents.executeJavaScript(`window.__ARKLOOP_DESKTOP__ = ${JSON.stringify({
            apiBaseUrl,
            mode: config.mode,
        })};`);
    });
}
let isQuitting = false;
electron_1.app.on('before-quit', () => {
    isQuitting = true;
});
electron_1.app.whenReady().then(async () => {
    const config = (0, config_1.loadConfig)();
    (0, ipc_1.registerIpcHandlers)(getWindow);
    // Local 模式下启动 sidecar
    if (config.mode === 'local') {
        (0, sidecar_1.setStatusListener)((s) => {
            mainWindow?.webContents.send('arkloop:sidecar:status-changed', s);
        });
        await (0, sidecar_1.startSidecar)(config.local.port);
    }
    mainWindow = createWindow();
    loadContent(mainWindow);
    (0, tray_1.createTray)(getWindow);
    (0, tray_1.registerGlobalShortcut)(getWindow);
});
electron_1.app.on('window-all-closed', () => {
    // macOS: 保持运行直到用户显式退出
    if (process.platform !== 'darwin') {
        electron_1.app.quit();
    }
});
electron_1.app.on('activate', () => {
    if (mainWindow) {
        mainWindow.show();
        mainWindow.focus();
    }
});
electron_1.app.on('will-quit', async () => {
    (0, tray_1.destroyTray)();
    await (0, sidecar_1.stopSidecar)();
});
//# sourceMappingURL=index.js.map