import { build } from 'esbuild';
import inlineWorkerPlugin from 'esbuild-plugin-inline-worker';

build({
    entryPoints: ['./js/captcha.js'],
    bundle: true,
    outfile: './static/js/privatecaptcha.js',
    loader: { '.css': 'text' },
    plugins: [
        inlineWorkerPlugin({
            minify: false
        }),
    ],
}).catch(() => process.exit(1));

