// Progressive enhancement: reading and navigation never depend on JavaScript.
for (const block of document.querySelectorAll('pre:has(code.language-bash)')) {
  const code = block.querySelector('code');
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'copy-button';
  button.textContent = 'Copy command';
  button.setAttribute('aria-label', 'Copy command to clipboard');
  button.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(code.textContent);
      button.textContent = 'Copied';
    } catch {
      const selection = window.getSelection();
      const range = document.createRange();
      range.selectNodeContents(code);
      selection.removeAllRanges();
      selection.addRange(range);
      button.textContent = 'Selected — copy with your browser';
    }
    setTimeout(() => { button.textContent = 'Copy command'; }, 2500);
  });
  block.prepend(button);
}
