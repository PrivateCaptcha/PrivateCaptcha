import { build, transform } from 'esbuild';
import { readFile } from "fs/promises"
import inlineWorkerPlugin from 'esbuild-plugin-inline-worker';

const stage = process.env.STAGE || 'dev';

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
}

build({
    entryPoints: ['./js/captcha.js'],
    bundle: true,
    outfile: './static/js/privatecaptcha.js',
    loader: { '.css': 'text' },
    plugins: [
        CSSMinifyPlugin,
        inlineWorkerPlugin({
            minify: config[stage].minify
        }),
    ],
    ...config[stage]
}).catch(() => process.exit(1));

