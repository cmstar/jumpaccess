declare module 'zmodem.js' {
  export interface Transfer {
    get_details(): { name: string; size: number }
    accept(options: { on_input: (data: number[] | Uint8Array) => void }): Promise<void>
    skip(): void
    send(data: Uint8Array): void
    end(data?: Uint8Array): Promise<void>
  }
  export interface Session {
    type: 'send' | 'receive'
    on(event: 'offer', callback: (offer: Transfer) => void): void
    on(event: 'session_end', callback: () => void): void
    start(): void
    send_offer(details: { name: string; size: number }): Promise<Transfer | undefined>
    close(): Promise<void>
    abort(): void
    aborted(): boolean
    has_ended(): boolean
  }
  export interface Detection {
    get_session_role(): 'send' | 'receive'
    is_valid(): boolean
    confirm(): Session
    deny(): void
  }
  export class Sentry {
    constructor(options: {
      to_terminal(data: number[]): void
      sender(data: number[]): void
      on_detect(detection: Detection): void
      on_retract(): void
    })
    consume(data: number[] | Uint8Array): void
    get_confirmed_session(): Session | null
  }
}
