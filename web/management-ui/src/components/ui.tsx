import type { ButtonHTMLAttributes, HTMLAttributes, InputHTMLAttributes, LabelHTMLAttributes, PropsWithChildren, SelectHTMLAttributes } from "react";

function cn(...values: Array<string | false | null | undefined>): string {
  return values.filter(Boolean).join(" ");
}

export function Shell({ children }: PropsWithChildren) {
  return (
    <div className="min-h-screen bg-stone-50 px-4 py-8 text-slate-900 sm:px-6 sm:py-10">
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-6">{children}</div>
    </div>
  );
}

export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "rounded-[28px] border border-slate-200/90 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.02),0_18px_50px_rgba(15,23,42,0.04)]",
        className,
      )}
      {...props}
    />
  );
}

export function CardHeader({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex flex-col gap-2 px-6 pt-6 sm:px-7 sm:pt-7", className)} {...props} />;
}

export function CardContent({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("px-6 pb-6 sm:px-7 sm:pb-7", className)} {...props} />;
}

export function CardTitle({ className, ...props }: HTMLAttributes<HTMLHeadingElement>) {
  return <h2 className={cn("text-xl font-semibold tracking-tight text-slate-950 sm:text-2xl", className)} {...props} />;
}

export function CardDescription({ className, ...props }: HTMLAttributes<HTMLParagraphElement>) {
  return <p className={cn("text-sm leading-6 text-slate-600", className)} {...props} />;
}

export function Button({
  className,
  variant = "default",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "default" | "secondary" | "ghost" | "destructive" }) {
  return (
    <button
      className={cn(
        "inline-flex h-10 items-center justify-center rounded-full px-4 text-sm font-medium transition duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-slate-300 focus-visible:ring-offset-2 focus-visible:ring-offset-stone-50 disabled:cursor-not-allowed disabled:opacity-50",
        variant === "default" && "bg-slate-950 text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.08)] hover:bg-slate-800",
        variant === "secondary" && "bg-slate-100 text-slate-900 hover:bg-slate-200",
        variant === "ghost" && "bg-transparent text-slate-700 hover:bg-slate-100/90",
        variant === "destructive" && "bg-red-600 text-white hover:bg-red-500",
        className,
      )}
      {...props}
    />
  );
}

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        "h-11 w-full rounded-2xl border border-slate-200 bg-white px-4 text-sm text-slate-900 outline-none transition placeholder:text-slate-400 focus:border-slate-400 focus:ring-2 focus:ring-slate-200",
        className,
      )}
      {...props}
    />
  );
}

export function Label({ className, ...props }: LabelHTMLAttributes<HTMLLabelElement>) {
  return <label className={cn("text-sm font-medium text-slate-700", className)} {...props} />;
}

export function Select({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={cn(
        "h-11 w-full appearance-none rounded-2xl border border-slate-200 bg-white px-4 text-sm text-slate-900 outline-none transition focus:border-slate-400 focus:ring-2 focus:ring-slate-200",
        className,
      )}
      {...props}
    />
  );
}

export function Badge({ className, ...props }: HTMLAttributes<HTMLSpanElement>) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-2 rounded-full border border-slate-200 bg-slate-50 px-3 py-1 text-xs font-medium text-slate-700",
        className,
      )}
      {...props}
    />
  );
}

export function Alert({
  className,
  tone = "neutral",
  ...props
}: HTMLAttributes<HTMLDivElement> & { tone?: "neutral" | "error" | "success" }) {
  return (
    <div
      className={cn(
        "rounded-2xl border px-4 py-3 text-sm",
        tone === "neutral" && "border-slate-200 bg-slate-50 text-slate-700",
        tone === "error" && "border-red-200 bg-red-50 text-red-700",
        tone === "success" && "border-emerald-200 bg-emerald-50 text-emerald-700",
        className,
      )}
      {...props}
    />
  );
}

export function Separator({ className, ...props }: HTMLAttributes<HTMLHRElement>) {
  return <hr className={cn("border-0 border-t border-slate-200", className)} {...props} />;
}

export function CollapsibleSection({
  className,
  title,
  children,
  defaultOpen = false,
}: PropsWithChildren<{ className?: string; title: string; defaultOpen?: boolean }>) {
  return (
    <details className={cn("rounded-2xl border border-slate-200 bg-slate-50/70 px-4 py-3", className)} open={defaultOpen}>
      <summary className="cursor-pointer list-none text-sm font-medium text-slate-700">{title}</summary>
      <div className="mt-4">{children}</div>
    </details>
  );
}
