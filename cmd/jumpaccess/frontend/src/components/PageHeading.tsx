import type { ReactNode } from 'react'
import { Activity, type LucideIcon } from 'lucide-react'

export function PageHeading({ children, description, eyebrow, title, icon: Icon = Activity }: { children?: ReactNode; description?: string; eyebrow: string; title: string; icon?: LucideIcon }) {
  return <div className="page-heading"><div><div className="eyebrow"><Icon />{eyebrow}</div><h1>{title}</h1>{description ? <p>{description}</p> : null}</div>{children}</div>
}
