import { build, transform } from 'esbuild';
import { readFile } from "fs/promises"
import inlineWorkerPlugin from 'esbuild-plugin-inline-worker';

const stage = process.env.STAGE || 'dev';
const buildTarget = process.env.BUILD_TARGET || 'default';

const config = {
  dev: {
    minify: false,
    sourcemap: true,
  },
  prod: {
    minify: true,
    sourcemap: true,
  }
};

let CSSMinifyPlugin = {
    name: "CSSMinifyPlugin",
    setup(build) {
        build.onLoad({ filter: /\.css$/ }, async (args) => {
            const f = await readFile(args.path)
            const css = await transform(f, { loader: "css", minify: true })
            return { loader: "text", contents: css.code }
        })
    }
};

let entryPointsConfig;
let outfileConfig;
let formatConfig;

if (buildTarget === 'library') {
  entryPointsConfig = ['./js/widget.js'];
  outfileConfig = './lib/index.js';
  formatConfig = 'esm';
} else { // 'default'
  entryPointsConfig = ['./js/captcha.js'];
  outfileConfig = './static/js/privatecaptcha.js';
  formatConfig = 'iife';
}

build({
    entryPoints: entryPointsConfig,
    bundle: true,
    outfile: outfileConfig,
    format: formatConfig,
    loader: { '.css': 'text', '.wasm': 'base64' },
    plugins: [
        CSSMinifyPlugin,
        inlineWorkerPlugin({
            minify: config[stage].minify,
            loader: { '.wasm': 'base64' },
        }),
    ],
    ...config[stage]
}).catch(() => process.exit(1));
