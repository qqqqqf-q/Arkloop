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
exports.createTray = createTray;
exports.registerGlobalShortcut = registerGlobalShortcut;
exports.destroyTray = destroyTray;
const electron_1 = require("electron");
const path = __importStar(require("path"));
let tray = null;
function getTrayIcon() {
    const iconName = process.platform === 'darwin' ? 'tray-icon.png' : 'tray-icon.png';
    const iconPath = electron_1.app.isPackaged
        ? path.join(process.resourcesPath, iconName)
        : path.join(__dirname, '..', '..', 'resources', iconName);
    try {
        const img = electron_1.nativeImage.createFromPath(iconPath);
        if (process.platform === 'darwin')
            img.setTemplateImage(true);
        return img;
    }
    catch {
        return electron_1.nativeImage.createEmpty();
    }
}
function createTray(getWindow) {
    tray = new electron_1.Tray(getTrayIcon());
    tray.setToolTip('Arkloop');
    const contextMenu = electron_1.Menu.buildFromTemplate([
        {
            label: 'Show',
            click: () => {
                const win = getWindow();
                if (win) {
                    win.show();
                    win.focus();
                }
            },
        },
        { type: 'separator' },
        {
            label: 'Quit',
            click: () => electron_1.app.quit(),
        },
    ]);
    tray.setContextMenu(contextMenu);
    tray.on('double-click', () => {
        const win = getWindow();
        if (win) {
            win.show();
            win.focus();
        }
    });
    return tray;
}
function registerGlobalShortcut(getWindow) {
    electron_1.globalShortcut.register('CommandOrControl+Shift+A', () => {
        const win = getWindow();
        if (!win)
            return;
        if (win.isVisible() && win.isFocused()) {
            win.hide();
        }
        else {
            win.show();
            win.focus();
        }
    });
}
function destroyTray() {
    if (tray) {
        tray.destroy();
        tray = null;
    }
    electron_1.globalShortcut.unregisterAll();
}
//# sourceMappingURL=tray.js.map