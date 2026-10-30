import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@router-forge/ui/styles.css";
import { TooltipProvider } from "@router-forge/ui/components/ui/tooltip";
import { App, initializeTheme } from "./App";

initializeTheme();
const queries = new QueryClient({ defaultOptions: { queries: { staleTime: 1500, retry: 1, refetchOnWindowFocus: true } } });
const root = document.getElementById("root");
if (!root) throw new Error("Application root is missing");
createRoot(root).render(<QueryClientProvider client={queries}><TooltipProvider><App /></TooltipProvider></QueryClientProvider>);
