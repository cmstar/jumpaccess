import { closeSync, openSync } from 'node:fs'
import { join } from 'node:path'

closeSync(openSync(join('dist', 'gitkeep'), 'w'))
