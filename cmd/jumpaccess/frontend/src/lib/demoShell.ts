// 仅处理预设命令；不执行本机命令，也不连接远程 Shell。
export class DemoShell {
  private line = ''
  private directory: string
  private escapeSequence = false

  constructor(private readonly username: string, private readonly hostname: string) {
    this.directory = `/home/${username}`
  }

  prompt() {
    return `\x1b]7;file://${this.hostname}${encodeURI(this.directory)}\x1b\\\x1b[32m${this.username}@${this.hostname}\x1b[0m:\x1b[34m${this.directory}\x1b[0m $ `
  }

  write(data: string) {
    let output = ''
    for (const character of data.replace(/\r\n/g, '\r')) {
      if (character === '\x1b') { this.escapeSequence = true; continue }
      if (this.escapeSequence) {
        if (/[A-Za-z~]/.test(character)) this.escapeSequence = false
        continue
      }
      if (character === '\r' || character === '\n') {
        output += `\r\n${this.execute(this.line.trim())}${this.prompt()}`
        this.line = ''
      } else if (character === '\x7f' || character === '\b') {
        if (this.line) { this.line = this.line.slice(0, -1); output += '\b \b' }
      } else if (character === '\x03') {
        this.line = ''
        output += `^C\r\n${this.prompt()}`
      } else if (character >= ' ') {
        this.line += character
        output += character
      }
    }
    return output
  }

  private execute(command: string): string {
    if (!command) return ''
    if (command === 'clear') return '\x1b[2J\x1b[H'
    if (command === 'help') return 'Sample commands: ls, ls -la, pwd, whoami, hostname, uname -a, uptime, df -h, cat app.yaml, cd /srv/webapp, clear\r\nAll output is simulated; files and servers are unchanged.\r\n'
    if (command === 'pwd') return `${this.directory}\r\n`
    if (command === 'whoami') return `${this.username}\r\n`
    if (command === 'hostname') return `${this.hostname}\r\n`
    if (command === 'uname -a') return `Linux ${this.hostname} 6.8.0-generic x86_64 GNU/Linux\r\n`
    if (command === 'uptime') return ' 10:24:00 up 12 days,  3:18,  2 users,  load average: 0.12, 0.08, 0.05\r\n'
    if (command === 'df -h') return 'Filesystem      Size  Used Avail Use% Mounted on\r\n/dev/vda1        80G   18G   62G  23% /\r\n/dev/vdb1       200G   46G  154G  23% /srv\r\n'
    if (command === 'ls') return '\x1b[34mlogs  projects\x1b[0m  app.yaml  README.md  notes.txt\r\n'
    if (command === 'ls -la') return 'total 24\r\ndrwxr-xr-x  2 deploy deploy 4096 Sep 12 10:24 logs\r\ndrwxr-xr-x  3 deploy deploy 4096 Sep 12 10:24 projects\r\n-rw-r--r--  1 deploy deploy 1840 Sep 12 10:24 app.yaml\r\n-rw-r--r--  1 deploy deploy 3200 Sep 12 10:24 README.md\r\n'
    if (command === 'cat app.yaml') return 'service: webapp\r\nenvironment: demonstration\r\nreplicas: 3\r\nlisten: 8080\r\n'
    if (command === 'cd /srv/webapp') { this.directory = '/srv/webapp'; return '' }
    if (command === 'cd' || command === 'cd ~') { this.directory = `/home/${this.username}`; return '' }
    return `Demo: unsupported command: ${command}\r\nType help for available sample commands.\r\n`
  }
}
