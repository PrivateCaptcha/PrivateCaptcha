import { build, transform } from 'esbuild';
import { readFile } from "fs/promises"
import { resolve } from 'node:path';
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

const blake2bWorkerSolverPlugin = {
    name: 'blake2b-worker-solver',
    setup(build) {
        build.onResolve({ filter: /^\.\/worker-solver\.js$/ }, () => ({
            path: resolve('./js/worker-solver-blake2b.js'),
        }));
    },
};

async function buildBundle(entryPoint, outfile, format, blake2bOnly = false) {
    await build({
        entryPoints: [entryPoint],
        bundle: true,
        outfile,
        format,
        loader: { '.css': 'text', '.wasm': 'base64' },
        plugins: [
            CSSMinifyPlugin,
            inlineWorkerPlugin({
                minify: config[stage].minify,
                loader: { '.wasm': 'base64' },
                plugins: blake2bOnly ? [blake2bWorkerSolverPlugin] : [],
            }),
        ],
        ...config[stage],
    });
}

async function main() {
    if (buildTarget === 'library') {
        await buildBundle('./js/widget.js', './lib/index.js', 'esm');
    } else {
        await buildBundle('./js/captcha.js', './static/js/privatecaptcha.js', 'iife', true);
        await buildBundle('./js/captcha.js', './static/js/privatecaptcha-ext.js', 'iife');
    }
}

main().catch(() => process.exit(1));
