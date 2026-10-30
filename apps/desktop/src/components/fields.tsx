import type { InputHTMLAttributes, ReactNode } from "react";
import { Field as FieldRoot, FieldContent, FieldDescription, FieldError, FieldLabel } from "@router-forge/ui/components/ui/field";
import { Input } from "@router-forge/ui/components/ui/input";

export function Field({ id, label, hint, error, children, className, disabled, orientation = "vertical" }: { id: string; label: string; hint?: string; error?: string; children: ReactNode; className?: string; disabled?: boolean; orientation?: "vertical" | "horizontal" }) {
  const feedback = error
    ? <FieldError id={`${id}-help`}>{error}</FieldError>
    : hint ? <FieldDescription id={`${id}-help`}>{hint}</FieldDescription> : null;
  return <FieldRoot orientation={orientation} data-invalid={Boolean(error) || undefined} data-disabled={disabled || undefined} aria-labelledby={`${id}-label`} aria-describedby={error || hint ? `${id}-help` : undefined} className={className}>
    {orientation === "horizontal" ? <>{children}<FieldContent><FieldLabel id={`${id}-label`} htmlFor={id}>{label}</FieldLabel>{feedback}</FieldContent></> : <><FieldLabel id={`${id}-label`} htmlFor={id}>{label}</FieldLabel>{children}{feedback}</>}
  </FieldRoot>;
}

export function TextField({ id, label, value, onChange, hint, error, disabled, "aria-describedby": describedBy, ...props }: Omit<InputHTMLAttributes<HTMLInputElement>, "value" | "onChange" | "id"> & { id: string; label: string; value: string; onChange: (value: string) => void; hint?: string; error?: string }) {
  const help = [describedBy, error || hint ? `${id}-help` : undefined].filter(Boolean).join(" ") || undefined;
  return <Field id={id} label={label} hint={hint} error={error} disabled={disabled}><Input {...props} id={id} value={value} onChange={event => onChange(event.target.value)} disabled={disabled} aria-invalid={Boolean(error)} aria-describedby={help} /></Field>;
}
