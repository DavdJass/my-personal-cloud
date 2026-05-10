import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { api, type AIStatus } from "./api";

// AIContext exposes whether the AI search module is enabled on the server.
// Components read this once via useAI() instead of polling /api/ai/status
// from every page that needs to know.
const AIContext = createContext<AIStatus | null>(null);

export function AIProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AIStatus | null>(null);

  useEffect(() => {
    api
      .aiStatus()
      .then(setStatus)
      .catch(() => setStatus({ enabled: false, model: "" }));
  }, []);

  return <AIContext.Provider value={status}>{children}</AIContext.Provider>;
}

// useAI returns null while the status is loading, then an AIStatus object.
// Treat null as "loading" — don't show AI-dependent UI until it resolves.
export function useAI(): AIStatus | null {
  return useContext(AIContext);
}
