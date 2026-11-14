import * as esbuild from 'esbuild'

await esbuild.build({
  entryPoints: ['src/main.js'],
  bundle: true,
  minify: true,
  format: 'esm',
  target: ['es2020'],
  outfile: '../public/app.js',
  legalComments: 'none',
  sourcemap: false,
  logLevel: 'info'
})
