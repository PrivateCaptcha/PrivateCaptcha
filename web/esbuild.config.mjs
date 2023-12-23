import { build } from 'esbuild';

build({
    entryPoints: ['./js/index.js'],
    bundle: true,
    outfile: './static/js/bundle.js'
}).catch(() => process.exit(1));

