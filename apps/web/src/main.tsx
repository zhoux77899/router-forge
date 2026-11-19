import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@router-forge/ui/styles.css";
import { initializeAppearance } from "@router-forge/ui/lib/appearance";
import { initializeSystemFavicon } from "./favicon";
import { App } from "./App";

initializeAppearance();
const stopSystemFavicon = initializeSystemFavicon();
if (import.meta.hot) import.meta.hot.dispose(stopSystemFavicon);
const queries = new QueryClient({ defaultOptions: { queries: { staleTime: 1500, retry: 1, refetchOnWindowFocus: true } } });
const root = document.getElementById("root");
if (!root) throw new Error("Application root is missing");
createRoot(root).render(<QueryClientProvider client={queries}><App /></QueryClientProvider>);
