import type { Directive } from "vue";

const cleanup = new WeakMap<HTMLElement, () => void>();
export const vModal: Directive<HTMLElement, () => void> = {
  mounted(el, binding) {
    const previous =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    const focusable = () =>
      Array.from(
        el.querySelectorAll<HTMLElement>(
          "button,a[href],input,select,textarea,[tabindex]",
        ),
      ).filter(
        (node) =>
          !node.hasAttribute("disabled") && node.getClientRects().length > 0,
      );
    queueMicrotask(() => {
      if (el.isConnected) focusable()[0]?.focus();
    });
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        binding.value();
        return;
      }
      if (event.key !== "Tab") return;
      const nodes = focusable();
      if (!nodes.length) return;
      const first = nodes[0],
        last = nodes[nodes.length - 1];
      if (event.shiftKey && document.activeElement === first) {
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
      if (previous?.isConnected) previous.focus();
    });
  },
  beforeUnmount(el) {
    cleanup.get(el)?.();
    cleanup.delete(el);
  },
};
