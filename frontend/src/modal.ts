import type { Directive } from "vue";

const cleanup = new WeakMap<HTMLElement, () => void>();
export const vModal: Directive<HTMLElement, () => void> = {
  mounted(el, binding) {
    const previous =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    const addedTabIndex = !el.hasAttribute("tabindex");
    if (addedTabIndex) el.setAttribute("tabindex", "-1");
    const focusable = () =>
      Array.from(
        el.querySelectorAll<HTMLElement>(
          "button,a[href],input,select,textarea,summary,[tabindex]",
        ),
      ).filter(
        (node) =>
          !node.matches(":disabled") &&
          (!node.hasAttribute("tabindex") || node.tabIndex >= 0) &&
          node.getClientRects().length > 0,
      );
    queueMicrotask(() => {
      if (el.isConnected) (focusable()[0] ?? el).focus();
    });
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        // Nested Escape previously bubbled into the outer modal and closed both.
        event.stopPropagation();
        binding.value();
        return;
      }
      if (event.key !== "Tab") return;
      // The outer trap must not process an inner modal's focus movement.
      event.stopPropagation();
      const nodes = focusable();
      if (!nodes.length) {
        event.preventDefault();
        event.stopPropagation();
        el.focus();
        return;
      }
      const first = nodes[0],
        last = nodes[nodes.length - 1];
      if (!nodes.includes(document.activeElement as HTMLElement)) {
        event.preventDefault();
        (event.shiftKey ? last : first).focus();
      } else if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    el.addEventListener("keydown", key);
    cleanup.set(el, () => {
      el.removeEventListener("keydown", key);
      if (addedTabIndex) el.removeAttribute("tabindex");
      if (previous?.isConnected) previous.focus();
    });
  },
  beforeUnmount(el) {
    cleanup.get(el)?.();
    cleanup.delete(el);
  },
};
