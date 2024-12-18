import { build } from 'esbuild';
import inlineWorkerPlugin from 'esbuild-plugin-inline-worker';

const stage = process.env.STAGE || 'dev';

const config = {
  dev: {
    minify: false,
    sourcemap: true,
  },
  prod: {
    minify: true,
    sourcemap: false,
  }
};

build({
    entryPoints: ['./js/captcha.js'],
    bundle: true,
    outfile: './static/js/privatecaptcha.js',
    loader: { '.css': 'text' },
    plugins: [
        inlineWorkerPlugin({
            minify: config[stage].minify
        }),
    ],
    ...config[stage]
}).catch(() => process.exit(1));

