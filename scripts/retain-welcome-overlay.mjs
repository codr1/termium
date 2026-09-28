import fs from 'node:fs/promises';
import path from 'node:path';

// The installer restores upstream Vimium separately. Keep the complete local
// welcome page, including its font files and licenses, in the app archive.
export async function retainWelcomeOverlay(extension) {
    const staged = await fs.mkdtemp(path.join(path.dirname(extension), '.termium-welcome-'));
    try {
        await fs.mkdir(path.join(staged, 'pages'));
        for (const entry of ['termium.js', 'termium.html', 'termium.css', 'termium-mark.svg', 'fonts.css', 'welcome-ready.js', 'fonts']) {
            await fs.cp(path.join(extension, 'pages', entry), path.join(staged, 'pages', entry), { recursive: true });
        }
        // Finish copying before removing anything: missing assets must fail
        // packaging rather than produce an incomplete welcome page.
        await fs.rm(extension, { recursive: true });
        await fs.rename(staged, extension);
    } finally {
        await fs.rm(staged, { recursive: true, force: true });
    }
}
