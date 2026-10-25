// One line of activity: what happened, to which device, when.
import { Link } from 'react-router-dom'
import type { NetEvent } from '../lib/types'
import { EVENT_ICON, eventTone } from '../lib/network'
import { fmtISOTime } from '../lib/format'
import { Icon } from './icons'

export function EventRow({ e, showDevice = true, showTime = true }: { e: NetEvent; showDevice?: boolean; showTime?: boolean }) {
  const body = (
    <>
      <span className={`ev-ico ${eventTone(e)}`}><Icon name={EVENT_ICON[e.kind] || 'dot'} size={14} /></span>
      <span className="grow ev-text">
        {showDevice && e.device_name && e.kind !== 'new' ? <><b>{e.device_name}</b> · {e.text.charAt(0).toLowerCase() + e.text.slice(1)}</> : e.text}
      </span>
      {showTime && <span className="muted small tnum ev-time">{fmtISOTime(e.at)}</span>}
    </>
  )
  return e.device_id && showDevice ? <Link to={`/devices/${e.device_id}`} className="list-row ev-row">{body}</Link> : <div className="list-row ev-row">{body}</div>
}
