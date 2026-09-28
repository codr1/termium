// Run in the head, after the stylesheets and before the first visible layout.
// A failed/slow font load must neither strand a blank page nor swap fonts later.
(() => {
  const root = document.documentElement;
  if (!document.fonts) return;
  root.classList.add('welcome-fonts-loading');
  let settled = false;
  const reveal = fallback => {
    if (settled) return;
    settled = true;
    clearTimeout(timer);
    if (fallback) root.classList.add('welcome-fonts-fallback');
    root.classList.remove('welcome-fonts-loading');
  };
  const timer = setTimeout(() => reveal(true), 1200);
  Promise.all([
    document.fonts.load('400 16px "JetBrains Mono"'),
    document.fonts.load('650 16px "Inter"'),
  ]).then(faces => reveal(faces.some(fonts => fonts.length === 0)), () => reveal(true));
})();
