// 独立进程中的现有 zmodem.js 对端，用于交叉验证 Go 协议实现。
const path = require('node:path')
const assert = require('node:assert/strict')
const Zmodem = require(path.resolve(__dirname, '../../..', 'cmd/jumpaccess/frontend/node_modules/zmodem.js'))
const mode = process.argv[2]
const size = Number(process.argv[3])
const expected = Buffer.from(Array.from({ length: size }, (_, i) => i % 256))
console.log = console.debug = (...args) => console.error(...args)
let session
const fail = error => { console.error(error); process.exit(1) }
const finish = () => process.stdout.write('shell$ ', () => process.exit(0))
const sentry = new Zmodem.Sentry({
  to_terminal: () => {}, on_retract: () => {},
  sender: data => process.stdout.write(Buffer.from(data)),
  on_detect: detection => {
    session = detection.confirm()
    if (mode === 'receive') {
      const chunks = []
      session.on('offer', offer => {
        offer.accept({ on_input: data => chunks.push(Buffer.from(data)) }).then(() => {
          assert.deepEqual(Buffer.concat(chunks), expected)
        }).catch(fail)
      })
      session.on('session_end', finish)
      session.start()
    } else {
      ;(async () => {
        const transfer = await session.send_offer({ name: 'binary.dat', size })
        for (let offset = 0; offset < size; offset += 8192) {
          transfer.send(expected.subarray(offset, offset + 8192))
          if (mode === 'send-pause') await new Promise(() => {})
        }
        await transfer.end()
        await session.close()
        finish()
      })().catch(fail)
    }
  },
})
process.stdin.on('data', data => { try { sentry.consume(data) } catch (e) { fail(e) } })
process.stdin.on('end', () => { if (!session?.has_ended()) fail(new Error('input ended before transfer')) })
setTimeout(() => fail(new Error('peer timeout')), 15000).unref()
if (mode === 'receive') sentry.consume(Buffer.from('**\x18B00000000000000\r\n\x11'))
else process.stdout.write('**\x18B00000000000000\r\n\x11')
