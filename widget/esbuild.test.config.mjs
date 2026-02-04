import { build, transform } from 'esbuild';
import { readFile } from "fs/promises"
import inlineWorkerPlugin from 'esbuild-plugin-inline-worker';

let CSSMinifyPlugin = {
    name: "CSSMinifyPlugin",
    setup(build) {
        build.onLoad({ filter: /\.css$/ }, async (args) => {
            const f = await readFile(args.path)
            const css = await transform(f, { loader: "css", minify: false })
            return { loader: "text", contents: css.code }
        })
    }
}

build({
    entryPoints: ['./test/widget.test.js'],
    bundle: true,
    outfile: './test/bundle.test.js',
    format: 'esm',
    loader: { '.css': 'text' },
    external: ['node:test', 'node:assert', 'node:http', 'happy-dom'],
    plugins: [
        CSSMinifyPlugin,
        inlineWorkerPlugin({
            minify: false
        }),
    ],
    minify: false,
    sourcemap: true,
}).catch(() => process.exit(1));
