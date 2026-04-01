declare global {
  interface Window {
    plausible: (
      event: string,
      options?: { props?: Record<string, string> },
    ) => void;
  }
}

export function track(event: string, props?: Record<string, string>) {
  if (typeof window.plausible === "function") {
    window.plausible(event, props ? { props } : undefined);
  }
}
