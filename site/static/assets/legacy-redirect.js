// Old numbered optimization fragments now point to their catalog entries.
export function installLegacyOptimizationRedirects(win) {
  const redirect = () => {
    if (!win.location.hash) return;
    let id;
    try {
      id = decodeURIComponent(win.location.hash.slice(1));
    } catch {
      return;
    }
    const target = win.document.getElementById(id)?.dataset.movedTo;
    if (target) win.location.replace(target);
  };

  redirect();
  win.addEventListener("hashchange", redirect);
}
