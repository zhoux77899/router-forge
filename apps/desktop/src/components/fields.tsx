import type { InputHTMLAttributes, ReactNode } from "react";
import { Label } from "@router-forge/ui/components/ui/label";
import { Input } from "@router-forge/ui/components/ui/input";

export function Field({ id, label, hint, error, children, className = "" }: { id: string; label: string; hint?: string; error?: string; children: ReactNode; className?: string }) {
  return <div data-invalid={Boolean(error) || undefined} className={`space-y-1.5 ${className}`}><Label htmlFor={id}>{label}</Label>{children}<div id={`${id}-help`} className="min-h-4 text-xs">{error ? <p className="text-destructive" role="alert">{error}</p> : hint && <p className="text-muted-foreground">{hint}</p>}</div></div>;
}

export function TextField({ id, label, value, onChange, hint, error, ...props }: Omit<InputHTMLAttributes<HTMLInputElement>, "value" | "onChange" | "id"> & { id: string; label: string; value: string; onChange: (value: string) => void; hint?: string; error?: string }) {
  return <Field id={id} label={label} hint={hint} error={error}><Input {...props} id={id} value={value} onChange={event => onChange(event.target.value)} aria-invalid={Boolean(error)} aria-describedby={`${id}-help`} /></Field>;
}
