"use strict";
(() => {
  // node_modules/base64-arraybuffer/dist/base64-arraybuffer.es5.js
  var chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
  var lookup = typeof Uint8Array === "undefined" ? [] : new Uint8Array(256);
  for (i = 0; i < chars.length; i++) {
    lookup[chars.charCodeAt(i)] = i;
  }
  var i;
  var encode = function(arraybuffer) {
    var bytes = new Uint8Array(arraybuffer), i, len = bytes.length, base64 = "";
    for (i = 0; i < len; i += 3) {
      base64 += chars[bytes[i] >> 2];
      base64 += chars[(bytes[i] & 3) << 4 | bytes[i + 1] >> 4];
      base64 += chars[(bytes[i + 1] & 15) << 2 | bytes[i + 2] >> 6];
      base64 += chars[bytes[i + 2] & 63];
    }
    if (len % 3 === 2) {
      base64 = base64.substring(0, base64.length - 1) + "=";
    } else if (len % 3 === 1) {
      base64 = base64.substring(0, base64.length - 2) + "==";
    }
    return base64;
  };
  var decode = function(base64) {
    var bufferLength = base64.length * 0.75, len = base64.length, i, p = 0, encoded1, encoded2, encoded3, encoded4;
    if (base64[base64.length - 1] === "=") {
      bufferLength--;
      if (base64[base64.length - 2] === "=") {
        bufferLength--;
      }
    }
    var arraybuffer = new ArrayBuffer(bufferLength), bytes = new Uint8Array(arraybuffer);
    for (i = 0; i < len; i += 4) {
      encoded1 = lookup[base64.charCodeAt(i)];
      encoded2 = lookup[base64.charCodeAt(i + 1)];
      encoded3 = lookup[base64.charCodeAt(i + 2)];
      encoded4 = lookup[base64.charCodeAt(i + 3)];
      bytes[p++] = encoded1 << 2 | encoded2 >> 4;
      bytes[p++] = (encoded2 & 15) << 4 | encoded3 >> 2;
      bytes[p++] = (encoded3 & 3) << 6 | encoded4 & 63;
    }
    return arraybuffer;
  };

  // js/puzzle.js
  var PUZZLE_BUFFER_LENGTH = 128;
  var ACCEPTABLE_CLIENT_ERRORS = [408, 409, 429];
  async function getPuzzle(endpoint, sitekey) {
    try {
      const response = await fetchWithBackoff(
        `${endpoint}?sitekey=${sitekey}`,
        { headers: [["x-pc-captcha-version", "1"]], mode: "cors" },
        3
        /*max attempts*/
      );
      if (response.ok) {
        const data = await response.text();
        return data;
      } else {
        let json = await response.json();
        if (json && json.error) {
          throw Error(json.error);
        }
      }
    } catch (err) {
      console.error("[privatecaptcha]", err);
      throw err;
    }
    throw Error("Internal error");
  }
  function wait(delay) {
    return new Promise((resolve) => setTimeout(resolve, delay));
  }
  async function fetchWithBackoff(url, options, maxAttempts, initialDelay = 800, maxDelay = 6e3) {
    for (let attempt = 0; attempt < maxAttempts; attempt++) {
      if (attempt > 0) {
        const delay = Math.min(initialDelay * Math.pow(2, attempt), maxDelay);
        await wait(delay);
      }
      try {
        const response = await fetch(url, options);
        if (response.ok) {
          return response;
        } else {
          console.warn("[privatecaptcha]", `HTTP request failed. status=${response.status}`);
        }
        if (response.status >= 400 && response.status < 500 && !ACCEPTABLE_CLIENT_ERRORS.includes(response.status)) {
          break;
        } else {
          continue;
        }
      } catch (err) {
        console.error("[privatecaptcha]", err);
      }
    }
    throw new Error("Maximum number of attempts exceeded");
  }
  function readUInt32LE(binaryData, offset) {
    return (binaryData[offset] | binaryData[offset + 1] << 8 | binaryData[offset + 2] << 16 | binaryData[offset + 3] << 24) >>> 0;
  }
  function readUInt64LE(binaryData, offset) {
    return BigInt(readUInt32LE(binaryData, offset)) + (BigInt(readUInt32LE(binaryData, offset + 4)) << 32n);
  }
  var Puzzle = class {
    constructor(rawData) {
      this.puzzleBuffer = null;
      this.ID = null;
      this.difficulty = null;
      this.solutionsCount = null;
      this.expirationTimestamp = null;
      this.userData = null;
      this.signature = null;
      this.parse(rawData);
      this.rawData = rawData;
    }
    parse(rawData) {
      const parts = rawData.split(".");
      if (parts.length !== 2) {
        throw Error(`Invalid amount of parts: ${parts.length}`);
      }
      const buffer = parts[0];
      this.signature = parts[1];
      const data = new Uint8Array(decode(buffer));
      let offset = 0;
      offset += 1;
      offset += 16;
      this.ID = readUInt64LE(data, offset);
      offset += 8;
      this.difficulty = data[offset];
      offset += 1;
      this.solutionsCount = data[offset];
      offset += 1;
      const timestamp = readUInt32LE(data, offset);
      this.expiration = new Date(timestamp * 1e3);
      offset += 4;
      offset += 4;
      const userDataSize = 16;
      this.userData = data.slice(offset, offset + userDataSize);
      offset += userDataSize;
      let puzzleBuffer = data;
      if (puzzleBuffer.length < PUZZLE_BUFFER_LENGTH) {
        const enlargedBuffer = new Uint8Array(PUZZLE_BUFFER_LENGTH);
        enlargedBuffer.set(puzzleBuffer);
        this.puzzleBuffer = enlargedBuffer;
      } else {
        this.puzzleBuffer = puzzleBuffer;
      }
    }
    expirationMillis() {
      const currentDate = /* @__PURE__ */ new Date();
      const diff = this.expiration - currentDate;
      return diff;
    }
  };

  // inline-worker:__inline-worker
  function inlineWorker(scriptText) {
    let blob = new Blob([scriptText], { type: "text/javascript" });
    let url = URL.createObjectURL(blob);
    let worker = new Worker(url);
    URL.revokeObjectURL(url);
    return worker;
  }

  // js/puzzle.worker.js
  function Worker2() {
    return inlineWorker('var __create = Object.create;\nvar __defProp = Object.defineProperty;\nvar __getOwnPropDesc = Object.getOwnPropertyDescriptor;\nvar __getOwnPropNames = Object.getOwnPropertyNames;\nvar __getProtoOf = Object.getPrototypeOf;\nvar __hasOwnProp = Object.prototype.hasOwnProperty;\nvar __commonJS = (cb, mod) => function __require() {\n  return mod || (0, cb[__getOwnPropNames(cb)[0]])((mod = { exports: {} }).exports, mod), mod.exports;\n};\nvar __copyProps = (to, from, except, desc) => {\n  if (from && typeof from === "object" || typeof from === "function") {\n    for (let key of __getOwnPropNames(from))\n      if (!__hasOwnProp.call(to, key) && key !== except)\n        __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });\n  }\n  return to;\n};\nvar __toESM = (mod, isNodeMode, target) => (target = mod != null ? __create(__getProtoOf(mod)) : {}, __copyProps(\n  // If the importer is in node compatibility mode or this is not an ESM\n  // file that has been converted to a CommonJS file using a Babel-\n  // compatible transform (i.e. "__esModule" has not been set), then set\n  // "default" to the CommonJS "module.exports" for node compatibility.\n  isNodeMode || !mod || !mod.__esModule ? __defProp(target, "default", { value: mod, enumerable: true }) : target,\n  mod\n));\n\n// node_modules/nanoassert/index.js\nvar require_nanoassert = __commonJS({\n  "node_modules/nanoassert/index.js"(exports, module) {\n    module.exports = assert;\n    var AssertionError = class extends Error {\n    };\n    AssertionError.prototype.name = "AssertionError";\n    function assert(t, m) {\n      if (!t) {\n        var err = new AssertionError(m);\n        if (Error.captureStackTrace)\n          Error.captureStackTrace(err, assert);\n        throw err;\n      }\n    }\n  }\n});\n\n// node_modules/b4a/lib/ascii.js\nvar require_ascii = __commonJS({\n  "node_modules/b4a/lib/ascii.js"(exports, module) {\n    function byteLength(string) {\n      return string.length;\n    }\n    function toString(buffer) {\n      const len = buffer.byteLength;\n      let result = "";\n      for (let i = 0; i < len; i++) {\n        result += String.fromCharCode(buffer[i]);\n      }\n      return result;\n    }\n    function write(buffer, string, offset = 0, length = byteLength(string)) {\n      const len = Math.min(length, buffer.byteLength - offset);\n      for (let i = 0; i < len; i++) {\n        buffer[offset + i] = string.charCodeAt(i);\n      }\n      return len;\n    }\n    module.exports = {\n      byteLength,\n      toString,\n      write\n    };\n  }\n});\n\n// node_modules/b4a/lib/base64.js\nvar require_base64 = __commonJS({\n  "node_modules/b4a/lib/base64.js"(exports, module) {\n    var alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";\n    var codes = new Uint8Array(256);\n    for (let i = 0; i < alphabet.length; i++) {\n      codes[alphabet.charCodeAt(i)] = i;\n    }\n    codes[\n      /* - */\n      45\n    ] = 62;\n    codes[\n      /* _ */\n      95\n    ] = 63;\n    function byteLength(string) {\n      let len = string.length;\n      if (string.charCodeAt(len - 1) === 61)\n        len--;\n      if (len > 1 && string.charCodeAt(len - 1) === 61)\n        len--;\n      return len * 3 >>> 2;\n    }\n    function toString(buffer) {\n      const len = buffer.byteLength;\n      let result = "";\n      for (let i = 0; i < len; i += 3) {\n        result += alphabet[buffer[i] >> 2] + alphabet[(buffer[i] & 3) << 4 | buffer[i + 1] >> 4] + alphabet[(buffer[i + 1] & 15) << 2 | buffer[i + 2] >> 6] + alphabet[buffer[i + 2] & 63];\n      }\n      if (len % 3 === 2) {\n        result = result.substring(0, result.length - 1) + "=";\n      } else if (len % 3 === 1) {\n        result = result.substring(0, result.length - 2) + "==";\n      }\n      return result;\n    }\n    function write(buffer, string, offset = 0, length = byteLength(string)) {\n      const len = Math.min(length, buffer.byteLength - offset);\n      for (let i = 0, j = 0; j < len; i += 4) {\n        const a = codes[string.charCodeAt(i)];\n        const b = codes[string.charCodeAt(i + 1)];\n        const c = codes[string.charCodeAt(i + 2)];\n        const d = codes[string.charCodeAt(i + 3)];\n        buffer[j++] = a << 2 | b >> 4;\n        buffer[j++] = (b & 15) << 4 | c >> 2;\n        buffer[j++] = (c & 3) << 6 | d & 63;\n      }\n      return len;\n    }\n    module.exports = {\n      byteLength,\n      toString,\n      write\n    };\n  }\n});\n\n// node_modules/b4a/lib/hex.js\nvar require_hex = __commonJS({\n  "node_modules/b4a/lib/hex.js"(exports, module) {\n    function byteLength(string) {\n      return string.length >>> 1;\n    }\n    function toString(buffer) {\n      const len = buffer.byteLength;\n      buffer = new DataView(buffer.buffer, buffer.byteOffset, len);\n      let result = "";\n      let i = 0;\n      for (let n = len - len % 4; i < n; i += 4) {\n        result += buffer.getUint32(i).toString(16).padStart(8, "0");\n      }\n      for (; i < len; i++) {\n        result += buffer.getUint8(i).toString(16).padStart(2, "0");\n      }\n      return result;\n    }\n    function write(buffer, string, offset = 0, length = byteLength(string)) {\n      const len = Math.min(length, buffer.byteLength - offset);\n      for (let i = 0; i < len; i++) {\n        const a = hexValue(string.charCodeAt(i * 2));\n        const b = hexValue(string.charCodeAt(i * 2 + 1));\n        if (a === void 0 || b === void 0) {\n          return buffer.subarray(0, i);\n        }\n        buffer[offset + i] = a << 4 | b;\n      }\n      return len;\n    }\n    module.exports = {\n      byteLength,\n      toString,\n      write\n    };\n    function hexValue(char) {\n      if (char >= 48 && char <= 57)\n        return char - 48;\n      if (char >= 65 && char <= 70)\n        return char - 65 + 10;\n      if (char >= 97 && char <= 102)\n        return char - 97 + 10;\n    }\n  }\n});\n\n// node_modules/b4a/lib/utf8.js\nvar require_utf8 = __commonJS({\n  "node_modules/b4a/lib/utf8.js"(exports, module) {\n    function byteLength(string) {\n      let length = 0;\n      for (let i = 0, n = string.length; i < n; i++) {\n        const code = string.charCodeAt(i);\n        if (code >= 55296 && code <= 56319 && i + 1 < n) {\n          const code2 = string.charCodeAt(i + 1);\n          if (code2 >= 56320 && code2 <= 57343) {\n            length += 4;\n            i++;\n            continue;\n          }\n        }\n        if (code <= 127)\n          length += 1;\n        else if (code <= 2047)\n          length += 2;\n        else\n          length += 3;\n      }\n      return length;\n    }\n    var toString;\n    if (typeof TextDecoder !== "undefined") {\n      const decoder = new TextDecoder();\n      toString = function toString2(buffer) {\n        return decoder.decode(buffer);\n      };\n    } else {\n      toString = function toString2(buffer) {\n        const len = buffer.byteLength;\n        let output = "";\n        let i = 0;\n        while (i < len) {\n          let byte = buffer[i];\n          if (byte <= 127) {\n            output += String.fromCharCode(byte);\n            i++;\n            continue;\n          }\n          let bytesNeeded = 0;\n          let codePoint = 0;\n          if (byte <= 223) {\n            bytesNeeded = 1;\n            codePoint = byte & 31;\n          } else if (byte <= 239) {\n            bytesNeeded = 2;\n            codePoint = byte & 15;\n          } else if (byte <= 244) {\n            bytesNeeded = 3;\n            codePoint = byte & 7;\n          }\n          if (len - i - bytesNeeded > 0) {\n            let k = 0;\n            while (k < bytesNeeded) {\n              byte = buffer[i + k + 1];\n              codePoint = codePoint << 6 | byte & 63;\n              k += 1;\n            }\n          } else {\n            codePoint = 65533;\n            bytesNeeded = len - i;\n          }\n          output += String.fromCodePoint(codePoint);\n          i += bytesNeeded + 1;\n        }\n        return output;\n      };\n    }\n    var write;\n    if (typeof TextEncoder !== "undefined") {\n      const encoder = new TextEncoder();\n      write = function write2(buffer, string, offset = 0, length = byteLength(string)) {\n        const len = Math.min(length, buffer.byteLength - offset);\n        encoder.encodeInto(string, buffer.subarray(offset, offset + len));\n        return len;\n      };\n    } else {\n      write = function write2(buffer, string, offset = 0, length = byteLength(string)) {\n        const len = Math.min(length, buffer.byteLength - offset);\n        buffer = buffer.subarray(offset, offset + len);\n        let i = 0;\n        let j = 0;\n        while (i < string.length) {\n          const code = string.codePointAt(i);\n          if (code <= 127) {\n            buffer[j++] = code;\n            i++;\n            continue;\n          }\n          let count = 0;\n          let bits = 0;\n          if (code <= 2047) {\n            count = 6;\n            bits = 192;\n          } else if (code <= 65535) {\n            count = 12;\n            bits = 224;\n          } else if (code <= 2097151) {\n            count = 18;\n            bits = 240;\n          }\n          buffer[j++] = bits | code >> count;\n          count -= 6;\n          while (count >= 0) {\n            buffer[j++] = 128 | code >> count & 63;\n            count -= 6;\n          }\n          i += code >= 65536 ? 2 : 1;\n        }\n        return len;\n      };\n    }\n    module.exports = {\n      byteLength,\n      toString,\n      write\n    };\n  }\n});\n\n// node_modules/b4a/lib/utf16le.js\nvar require_utf16le = __commonJS({\n  "node_modules/b4a/lib/utf16le.js"(exports, module) {\n    function byteLength(string) {\n      return string.length * 2;\n    }\n    function toString(buffer) {\n      const len = buffer.byteLength;\n      let result = "";\n      for (let i = 0; i < len - 1; i += 2) {\n        result += String.fromCharCode(buffer[i] + buffer[i + 1] * 256);\n      }\n      return result;\n    }\n    function write(buffer, string, offset = 0, length = byteLength(string)) {\n      const len = Math.min(length, buffer.byteLength - offset);\n      let units = len;\n      for (let i = 0; i < string.length; ++i) {\n        if ((units -= 2) < 0)\n          break;\n        const c = string.charCodeAt(i);\n        const hi = c >> 8;\n        const lo = c % 256;\n        buffer[offset + i * 2] = lo;\n        buffer[offset + i * 2 + 1] = hi;\n      }\n      return len;\n    }\n    module.exports = {\n      byteLength,\n      toString,\n      write\n    };\n  }\n});\n\n// node_modules/b4a/browser.js\nvar require_browser = __commonJS({\n  "node_modules/b4a/browser.js"(exports, module) {\n    var ascii = require_ascii();\n    var base64 = require_base64();\n    var hex = require_hex();\n    var utf8 = require_utf8();\n    var utf16le = require_utf16le();\n    var LE = new Uint8Array(Uint16Array.of(255).buffer)[0] === 255;\n    function codecFor(encoding) {\n      switch (encoding) {\n        case "ascii":\n          return ascii;\n        case "base64":\n          return base64;\n        case "hex":\n          return hex;\n        case "utf8":\n        case "utf-8":\n        case void 0:\n          return utf8;\n        case "ucs2":\n        case "ucs-2":\n        case "utf16le":\n        case "utf-16le":\n          return utf16le;\n        default:\n          throw new Error(`Unknown encoding: ${encoding}`);\n      }\n    }\n    function isBuffer(value) {\n      return value instanceof Uint8Array;\n    }\n    function isEncoding(encoding) {\n      try {\n        codecFor(encoding);\n        return true;\n      } catch (e) {\n        return false;\n      }\n    }\n    function alloc(size, fill2, encoding) {\n      const buffer = new Uint8Array(size);\n      if (fill2 !== void 0)\n        exports.fill(buffer, fill2, 0, buffer.byteLength, encoding);\n      return buffer;\n    }\n    function allocUnsafe(size) {\n      return new Uint8Array(size);\n    }\n    function allocUnsafeSlow(size) {\n      return new Uint8Array(size);\n    }\n    function byteLength(string, encoding) {\n      return codecFor(encoding).byteLength(string);\n    }\n    function compare(a, b) {\n      if (a === b)\n        return 0;\n      const len = Math.min(a.byteLength, b.byteLength);\n      a = new DataView(a.buffer, a.byteOffset, a.byteLength);\n      b = new DataView(b.buffer, b.byteOffset, b.byteLength);\n      let i = 0;\n      for (let n = len - len % 4; i < n; i += 4) {\n        const x = a.getUint32(i, LE);\n        const y = b.getUint32(i, LE);\n        if (x !== y)\n          break;\n      }\n      for (; i < len; i++) {\n        const x = a.getUint8(i);\n        const y = b.getUint8(i);\n        if (x < y)\n          return -1;\n        if (x > y)\n          return 1;\n      }\n      return a.byteLength > b.byteLength ? 1 : a.byteLength < b.byteLength ? -1 : 0;\n    }\n    function concat(buffers, totalLength) {\n      if (totalLength === void 0) {\n        totalLength = buffers.reduce((len, buffer) => len + buffer.byteLength, 0);\n      }\n      const result = new Uint8Array(totalLength);\n      let offset = 0;\n      for (const buffer of buffers) {\n        if (offset + buffer.byteLength > result.byteLength) {\n          const sub = buffer.subarray(0, result.byteLength - offset);\n          result.set(sub, offset);\n          return result;\n        }\n        result.set(buffer, offset);\n        offset += buffer.byteLength;\n      }\n      return result;\n    }\n    function copy(source, target, targetStart = 0, start = 0, end = source.byteLength) {\n      if (end > 0 && end < start)\n        return 0;\n      if (end === start)\n        return 0;\n      if (source.byteLength === 0 || target.byteLength === 0)\n        return 0;\n      if (targetStart < 0)\n        throw new RangeError("targetStart is out of range");\n      if (start < 0 || start >= source.byteLength)\n        throw new RangeError("sourceStart is out of range");\n      if (end < 0)\n        throw new RangeError("sourceEnd is out of range");\n      if (targetStart >= target.byteLength)\n        targetStart = target.byteLength;\n      if (end > source.byteLength)\n        end = source.byteLength;\n      if (target.byteLength - targetStart < end - start) {\n        end = target.length - targetStart + start;\n      }\n      const len = end - start;\n      if (source === target) {\n        target.copyWithin(targetStart, start, end);\n      } else {\n        target.set(source.subarray(start, end), targetStart);\n      }\n      return len;\n    }\n    function equals(a, b) {\n      if (a === b)\n        return true;\n      if (a.byteLength !== b.byteLength)\n        return false;\n      const len = a.byteLength;\n      a = new DataView(a.buffer, a.byteOffset, a.byteLength);\n      b = new DataView(b.buffer, b.byteOffset, b.byteLength);\n      let i = 0;\n      for (let n = len - len % 4; i < n; i += 4) {\n        if (a.getUint32(i, LE) !== b.getUint32(i, LE))\n          return false;\n      }\n      for (; i < len; i++) {\n        if (a.getUint8(i) !== b.getUint8(i))\n          return false;\n      }\n      return true;\n    }\n    function fill(buffer, value, offset, end, encoding) {\n      if (typeof value === "string") {\n        if (typeof offset === "string") {\n          encoding = offset;\n          offset = 0;\n          end = buffer.byteLength;\n        } else if (typeof end === "string") {\n          encoding = end;\n          end = buffer.byteLength;\n        }\n      } else if (typeof value === "number") {\n        value = value & 255;\n      } else if (typeof value === "boolean") {\n        value = +value;\n      }\n      if (offset < 0 || buffer.byteLength < offset || buffer.byteLength < end) {\n        throw new RangeError("Out of range index");\n      }\n      if (offset === void 0)\n        offset = 0;\n      if (end === void 0)\n        end = buffer.byteLength;\n      if (end <= offset)\n        return buffer;\n      if (!value)\n        value = 0;\n      if (typeof value === "number") {\n        for (let i = offset; i < end; ++i) {\n          buffer[i] = value;\n        }\n      } else {\n        value = isBuffer(value) ? value : from(value, encoding);\n        const len = value.byteLength;\n        for (let i = 0; i < end - offset; ++i) {\n          buffer[i + offset] = value[i % len];\n        }\n      }\n      return buffer;\n    }\n    function from(value, encodingOrOffset, length) {\n      if (typeof value === "string")\n        return fromString(value, encodingOrOffset);\n      if (Array.isArray(value))\n        return fromArray(value);\n      if (ArrayBuffer.isView(value))\n        return fromBuffer(value);\n      return fromArrayBuffer(value, encodingOrOffset, length);\n    }\n    function fromString(string, encoding) {\n      const codec = codecFor(encoding);\n      const buffer = new Uint8Array(codec.byteLength(string));\n      codec.write(buffer, string, 0, buffer.byteLength);\n      return buffer;\n    }\n    function fromArray(array) {\n      const buffer = new Uint8Array(array.length);\n      buffer.set(array);\n      return buffer;\n    }\n    function fromBuffer(buffer) {\n      const copy2 = new Uint8Array(buffer.byteLength);\n      copy2.set(buffer);\n      return copy2;\n    }\n    function fromArrayBuffer(arrayBuffer, byteOffset, length) {\n      return new Uint8Array(arrayBuffer, byteOffset, length);\n    }\n    function includes(buffer, value, byteOffset, encoding) {\n      return indexOf(buffer, value, byteOffset, encoding) !== -1;\n    }\n    function bidirectionalIndexOf(buffer, value, byteOffset, encoding, first) {\n      if (buffer.byteLength === 0)\n        return -1;\n      if (typeof byteOffset === "string") {\n        encoding = byteOffset;\n        byteOffset = 0;\n      } else if (byteOffset === void 0) {\n        byteOffset = first ? 0 : buffer.length - 1;\n      } else if (byteOffset < 0) {\n        byteOffset += buffer.byteLength;\n      }\n      if (byteOffset >= buffer.byteLength) {\n        if (first)\n          return -1;\n        else\n          byteOffset = buffer.byteLength - 1;\n      } else if (byteOffset < 0) {\n        if (first)\n          byteOffset = 0;\n        else\n          return -1;\n      }\n      if (typeof value === "string") {\n        value = from(value, encoding);\n      } else if (typeof value === "number") {\n        value = value & 255;\n        if (first) {\n          return buffer.indexOf(value, byteOffset);\n        } else {\n          return buffer.lastIndexOf(value, byteOffset);\n        }\n      }\n      if (value.byteLength === 0)\n        return -1;\n      if (first) {\n        let foundIndex = -1;\n        for (let i = byteOffset; i < buffer.byteLength; i++) {\n          if (buffer[i] === value[foundIndex === -1 ? 0 : i - foundIndex]) {\n            if (foundIndex === -1)\n              foundIndex = i;\n            if (i - foundIndex + 1 === value.byteLength)\n              return foundIndex;\n          } else {\n            if (foundIndex !== -1)\n              i -= i - foundIndex;\n            foundIndex = -1;\n          }\n        }\n      } else {\n        if (byteOffset + value.byteLength > buffer.byteLength) {\n          byteOffset = buffer.byteLength - value.byteLength;\n        }\n        for (let i = byteOffset; i >= 0; i--) {\n          let found = true;\n          for (let j = 0; j < value.byteLength; j++) {\n            if (buffer[i + j] !== value[j]) {\n              found = false;\n              break;\n            }\n          }\n          if (found)\n            return i;\n        }\n      }\n      return -1;\n    }\n    function indexOf(buffer, value, byteOffset, encoding) {\n      return bidirectionalIndexOf(\n        buffer,\n        value,\n        byteOffset,\n        encoding,\n        true\n        /* first */\n      );\n    }\n    function lastIndexOf(buffer, value, byteOffset, encoding) {\n      return bidirectionalIndexOf(\n        buffer,\n        value,\n        byteOffset,\n        encoding,\n        false\n        /* last */\n      );\n    }\n    function swap(buffer, n, m) {\n      const i = buffer[n];\n      buffer[n] = buffer[m];\n      buffer[m] = i;\n    }\n    function swap16(buffer) {\n      const len = buffer.byteLength;\n      if (len % 2 !== 0)\n        throw new RangeError("Buffer size must be a multiple of 16-bits");\n      for (let i = 0; i < len; i += 2)\n        swap(buffer, i, i + 1);\n      return buffer;\n    }\n    function swap32(buffer) {\n      const len = buffer.byteLength;\n      if (len % 4 !== 0)\n        throw new RangeError("Buffer size must be a multiple of 32-bits");\n      for (let i = 0; i < len; i += 4) {\n        swap(buffer, i, i + 3);\n        swap(buffer, i + 1, i + 2);\n      }\n      return buffer;\n    }\n    function swap64(buffer) {\n      const len = buffer.byteLength;\n      if (len % 8 !== 0)\n        throw new RangeError("Buffer size must be a multiple of 64-bits");\n      for (let i = 0; i < len; i += 8) {\n        swap(buffer, i, i + 7);\n        swap(buffer, i + 1, i + 6);\n        swap(buffer, i + 2, i + 5);\n        swap(buffer, i + 3, i + 4);\n      }\n      return buffer;\n    }\n    function toBuffer(buffer) {\n      return buffer;\n    }\n    function toString(buffer, encoding, start = 0, end = buffer.byteLength) {\n      const len = buffer.byteLength;\n      if (start >= len)\n        return "";\n      if (end <= start)\n        return "";\n      if (start < 0)\n        start = 0;\n      if (end > len)\n        end = len;\n      if (start !== 0 || end < len)\n        buffer = buffer.subarray(start, end);\n      return codecFor(encoding).toString(buffer);\n    }\n    function write(buffer, string, offset, length, encoding) {\n      if (offset === void 0) {\n        encoding = "utf8";\n      } else if (length === void 0 && typeof offset === "string") {\n        encoding = offset;\n        offset = void 0;\n      } else if (encoding === void 0 && typeof length === "string") {\n        encoding = length;\n        length = void 0;\n      }\n      return codecFor(encoding).write(buffer, string, offset, length);\n    }\n    function writeDoubleLE(buffer, value, offset) {\n      if (offset === void 0)\n        offset = 0;\n      const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);\n      view.setFloat64(offset, value, true);\n      return offset + 8;\n    }\n    function writeFloatLE(buffer, value, offset) {\n      if (offset === void 0)\n        offset = 0;\n      const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);\n      view.setFloat32(offset, value, true);\n      return offset + 4;\n    }\n    function writeUInt32LE(buffer, value, offset) {\n      if (offset === void 0)\n        offset = 0;\n      const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);\n      view.setUint32(offset, value, true);\n      return offset + 4;\n    }\n    function writeInt32LE(buffer, value, offset) {\n      if (offset === void 0)\n        offset = 0;\n      const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);\n      view.setInt32(offset, value, true);\n      return offset + 4;\n    }\n    function readDoubleLE(buffer, offset) {\n      if (offset === void 0)\n        offset = 0;\n      const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);\n      return view.getFloat64(offset, true);\n    }\n    function readFloatLE(buffer, offset) {\n      if (offset === void 0)\n        offset = 0;\n      const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);\n      return view.getFloat32(offset, true);\n    }\n    function readUInt32LE2(buffer, offset) {\n      if (offset === void 0)\n        offset = 0;\n      const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);\n      return view.getUint32(offset, true);\n    }\n    function readInt32LE(buffer, offset) {\n      if (offset === void 0)\n        offset = 0;\n      const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);\n      return view.getInt32(offset, true);\n    }\n    module.exports = exports = {\n      isBuffer,\n      isEncoding,\n      alloc,\n      allocUnsafe,\n      allocUnsafeSlow,\n      byteLength,\n      compare,\n      concat,\n      copy,\n      equals,\n      fill,\n      from,\n      includes,\n      indexOf,\n      lastIndexOf,\n      swap16,\n      swap32,\n      swap64,\n      toBuffer,\n      toString,\n      write,\n      writeDoubleLE,\n      writeFloatLE,\n      writeUInt32LE,\n      writeInt32LE,\n      readDoubleLE,\n      readFloatLE,\n      readUInt32LE: readUInt32LE2,\n      readInt32LE\n    };\n  }\n});\n\n// node_modules/blake2b-wasm/blake2b.js\nvar require_blake2b = __commonJS({\n  "node_modules/blake2b-wasm/blake2b.js"(exports, module) {\n    var __commonJS2 = (cb, mod) => function __require() {\n      return mod || (0, cb[Object.keys(cb)[0]])((mod = { exports: {} }).exports, mod), mod.exports;\n    };\n    var __toBinary = /* @__PURE__ */ (() => {\n      var table = new Uint8Array(128);\n      for (var i = 0; i < 64; i++)\n        table[i < 26 ? i + 65 : i < 52 ? i + 71 : i < 62 ? i - 4 : i * 4 - 205] = i;\n      return (base64) => {\n        var n = base64.length, bytes2 = new Uint8Array((n - (base64[n - 1] == "=") - (base64[n - 2] == "=")) * 3 / 4 | 0);\n        for (var i2 = 0, j = 0; i2 < n; ) {\n          var c0 = table[base64.charCodeAt(i2++)], c1 = table[base64.charCodeAt(i2++)];\n          var c2 = table[base64.charCodeAt(i2++)], c3 = table[base64.charCodeAt(i2++)];\n          bytes2[j++] = c0 << 2 | c1 >> 4;\n          bytes2[j++] = c1 << 4 | c2 >> 2;\n          bytes2[j++] = c2 << 6 | c3;\n        }\n        return bytes2;\n      };\n    })();\n    var require_blake2b3 = __commonJS2({\n      "wasm-binary:./blake2b.wat"(exports2, module2) {\n        module2.exports = __toBinary("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");\n      }\n    });\n    var bytes = require_blake2b3();\n    var compiled = WebAssembly.compile(bytes);\n    module.exports = async (imports) => {\n      const instance = await WebAssembly.instantiate(await compiled, imports);\n      return instance.exports;\n    };\n  }\n});\n\n// node_modules/blake2b-wasm/index.js\nvar require_blake2b_wasm = __commonJS({\n  "node_modules/blake2b-wasm/index.js"(exports, module) {\n    var assert = require_nanoassert();\n    var b4a = require_browser();\n    var wasm = null;\n    var wasmPromise = typeof WebAssembly !== "undefined" && require_blake2b()().then((mod) => {\n      wasm = mod;\n    });\n    var head = 64;\n    var freeList = [];\n    module.exports = Blake2b;\n    var BYTES_MIN = module.exports.BYTES_MIN = 16;\n    var BYTES_MAX = module.exports.BYTES_MAX = 64;\n    var BYTES = module.exports.BYTES = 32;\n    var KEYBYTES_MIN = module.exports.KEYBYTES_MIN = 16;\n    var KEYBYTES_MAX = module.exports.KEYBYTES_MAX = 64;\n    var KEYBYTES = module.exports.KEYBYTES = 32;\n    var SALTBYTES = module.exports.SALTBYTES = 16;\n    var PERSONALBYTES = module.exports.PERSONALBYTES = 16;\n    function Blake2b(digestLength, key, salt, personal, noAssert) {\n      if (!(this instanceof Blake2b))\n        return new Blake2b(digestLength, key, salt, personal, noAssert);\n      if (!wasm)\n        throw new Error("WASM not loaded. Wait for Blake2b.ready(cb)");\n      if (!digestLength)\n        digestLength = 32;\n      if (noAssert !== true) {\n        assert(digestLength >= BYTES_MIN, "digestLength must be at least " + BYTES_MIN + ", was given " + digestLength);\n        assert(digestLength <= BYTES_MAX, "digestLength must be at most " + BYTES_MAX + ", was given " + digestLength);\n        if (key != null) {\n          assert(key instanceof Uint8Array, "key must be Uint8Array or Buffer");\n          assert(key.length >= KEYBYTES_MIN, "key must be at least " + KEYBYTES_MIN + ", was given " + key.length);\n          assert(key.length <= KEYBYTES_MAX, "key must be at least " + KEYBYTES_MAX + ", was given " + key.length);\n        }\n        if (salt != null) {\n          assert(salt instanceof Uint8Array, "salt must be Uint8Array or Buffer");\n          assert(salt.length === SALTBYTES, "salt must be exactly " + SALTBYTES + ", was given " + salt.length);\n        }\n        if (personal != null) {\n          assert(personal instanceof Uint8Array, "personal must be Uint8Array or Buffer");\n          assert(personal.length === PERSONALBYTES, "personal must be exactly " + PERSONALBYTES + ", was given " + personal.length);\n        }\n      }\n      if (!freeList.length) {\n        freeList.push(head);\n        head += 216;\n      }\n      this.digestLength = digestLength;\n      this.finalized = false;\n      this.pointer = freeList.pop();\n      this._memory = new Uint8Array(wasm.memory.buffer);\n      this._memory.fill(0, 0, 64);\n      this._memory[0] = this.digestLength;\n      this._memory[1] = key ? key.length : 0;\n      this._memory[2] = 1;\n      this._memory[3] = 1;\n      if (salt)\n        this._memory.set(salt, 32);\n      if (personal)\n        this._memory.set(personal, 48);\n      if (this.pointer + 216 > this._memory.length)\n        this._realloc(this.pointer + 216);\n      wasm.blake2b_init(this.pointer, this.digestLength);\n      if (key) {\n        this.update(key);\n        this._memory.fill(0, head, head + key.length);\n        this._memory[this.pointer + 200] = 128;\n      }\n    }\n    Blake2b.prototype._realloc = function(size) {\n      wasm.memory.grow(Math.max(0, Math.ceil(Math.abs(size - this._memory.length) / 65536)));\n      this._memory = new Uint8Array(wasm.memory.buffer);\n    };\n    Blake2b.prototype.update = function(input) {\n      assert(this.finalized === false, "Hash instance finalized");\n      assert(input instanceof Uint8Array, "input must be Uint8Array or Buffer");\n      if (head + input.length > this._memory.length)\n        this._realloc(head + input.length);\n      this._memory.set(input, head);\n      wasm.blake2b_update(this.pointer, head, head + input.length);\n      return this;\n    };\n    Blake2b.prototype.digest = function(enc) {\n      assert(this.finalized === false, "Hash instance finalized");\n      this.finalized = true;\n      freeList.push(this.pointer);\n      wasm.blake2b_final(this.pointer);\n      if (!enc || enc === "binary") {\n        return this._memory.slice(this.pointer + 128, this.pointer + 128 + this.digestLength);\n      }\n      if (typeof enc === "string") {\n        return b4a.toString(this._memory, enc, this.pointer + 128, this.pointer + 128 + this.digestLength);\n      }\n      assert(enc instanceof Uint8Array && enc.length >= this.digestLength, "input must be Uint8Array or Buffer");\n      for (var i = 0; i < this.digestLength; i++) {\n        enc[i] = this._memory[this.pointer + 128 + i];\n      }\n      return enc;\n    };\n    Blake2b.prototype.final = Blake2b.prototype.digest;\n    Blake2b.WASM = wasm;\n    Blake2b.SUPPORTED = typeof WebAssembly !== "undefined";\n    Blake2b.ready = function(cb) {\n      if (!cb)\n        cb = noop;\n      if (!wasmPromise)\n        return cb(new Error("WebAssembly not supported"));\n      return wasmPromise.then(() => cb(), cb);\n    };\n    Blake2b.prototype.ready = Blake2b.ready;\n    Blake2b.prototype.getPartialHash = function() {\n      return this._memory.slice(this.pointer, this.pointer + 216);\n    };\n    Blake2b.prototype.setPartialHash = function(ph) {\n      this._memory.set(ph, this.pointer);\n    };\n    function noop() {\n    }\n  }\n});\n\n// node_modules/blakejs/util.js\nvar require_util = __commonJS({\n  "node_modules/blakejs/util.js"(exports, module) {\n    var ERROR_MSG_INPUT = "Input must be an string, Buffer or Uint8Array";\n    function normalizeInput(input) {\n      let ret;\n      if (input instanceof Uint8Array) {\n        ret = input;\n      } else if (typeof input === "string") {\n        const encoder = new TextEncoder();\n        ret = encoder.encode(input);\n      } else {\n        throw new Error(ERROR_MSG_INPUT);\n      }\n      return ret;\n    }\n    function toHex(bytes) {\n      return Array.prototype.map.call(bytes, function(n) {\n        return (n < 16 ? "0" : "") + n.toString(16);\n      }).join("");\n    }\n    function uint32ToHex(val) {\n      return (4294967296 + val).toString(16).substring(1);\n    }\n    function debugPrint(label, arr, size) {\n      let msg = "\\n" + label + " = ";\n      for (let i = 0; i < arr.length; i += 2) {\n        if (size === 32) {\n          msg += uint32ToHex(arr[i]).toUpperCase();\n          msg += " ";\n          msg += uint32ToHex(arr[i + 1]).toUpperCase();\n        } else if (size === 64) {\n          msg += uint32ToHex(arr[i + 1]).toUpperCase();\n          msg += uint32ToHex(arr[i]).toUpperCase();\n        } else\n          throw new Error("Invalid size " + size);\n        if (i % 6 === 4) {\n          msg += "\\n" + new Array(label.length + 4).join(" ");\n        } else if (i < arr.length - 2) {\n          msg += " ";\n        }\n      }\n      console.log(msg);\n    }\n    function testSpeed(hashFn, N, M) {\n      let startMs = (/* @__PURE__ */ new Date()).getTime();\n      const input = new Uint8Array(N);\n      for (let i = 0; i < N; i++) {\n        input[i] = i % 256;\n      }\n      const genMs = (/* @__PURE__ */ new Date()).getTime();\n      console.log("Generated random input in " + (genMs - startMs) + "ms");\n      startMs = genMs;\n      for (let i = 0; i < M; i++) {\n        const hashHex = hashFn(input);\n        const hashMs = (/* @__PURE__ */ new Date()).getTime();\n        const ms = hashMs - startMs;\n        startMs = hashMs;\n        console.log("Hashed in " + ms + "ms: " + hashHex.substring(0, 20) + "...");\n        console.log(\n          Math.round(N / (1 << 20) / (ms / 1e3) * 100) / 100 + " MB PER SECOND"\n        );\n      }\n    }\n    module.exports = {\n      normalizeInput,\n      toHex,\n      debugPrint,\n      testSpeed\n    };\n  }\n});\n\n// node_modules/blakejs/blake2b.js\nvar require_blake2b2 = __commonJS({\n  "node_modules/blakejs/blake2b.js"(exports, module) {\n    var util = require_util();\n    function ADD64AA(v2, a, b) {\n      const o0 = v2[a] + v2[b];\n      let o1 = v2[a + 1] + v2[b + 1];\n      if (o0 >= 4294967296) {\n        o1++;\n      }\n      v2[a] = o0;\n      v2[a + 1] = o1;\n    }\n    function ADD64AC(v2, a, b0, b1) {\n      let o0 = v2[a] + b0;\n      if (b0 < 0) {\n        o0 += 4294967296;\n      }\n      let o1 = v2[a + 1] + b1;\n      if (o0 >= 4294967296) {\n        o1++;\n      }\n      v2[a] = o0;\n      v2[a + 1] = o1;\n    }\n    function B2B_GET32(arr, i) {\n      return arr[i] ^ arr[i + 1] << 8 ^ arr[i + 2] << 16 ^ arr[i + 3] << 24;\n    }\n    function B2B_G(a, b, c, d, ix, iy) {\n      const x0 = m[ix];\n      const x1 = m[ix + 1];\n      const y0 = m[iy];\n      const y1 = m[iy + 1];\n      ADD64AA(v, a, b);\n      ADD64AC(v, a, x0, x1);\n      let xor0 = v[d] ^ v[a];\n      let xor1 = v[d + 1] ^ v[a + 1];\n      v[d] = xor1;\n      v[d + 1] = xor0;\n      ADD64AA(v, c, d);\n      xor0 = v[b] ^ v[c];\n      xor1 = v[b + 1] ^ v[c + 1];\n      v[b] = xor0 >>> 24 ^ xor1 << 8;\n      v[b + 1] = xor1 >>> 24 ^ xor0 << 8;\n      ADD64AA(v, a, b);\n      ADD64AC(v, a, y0, y1);\n      xor0 = v[d] ^ v[a];\n      xor1 = v[d + 1] ^ v[a + 1];\n      v[d] = xor0 >>> 16 ^ xor1 << 16;\n      v[d + 1] = xor1 >>> 16 ^ xor0 << 16;\n      ADD64AA(v, c, d);\n      xor0 = v[b] ^ v[c];\n      xor1 = v[b + 1] ^ v[c + 1];\n      v[b] = xor1 >>> 31 ^ xor0 << 1;\n      v[b + 1] = xor0 >>> 31 ^ xor1 << 1;\n    }\n    var BLAKE2B_IV32 = new Uint32Array([\n      4089235720,\n      1779033703,\n      2227873595,\n      3144134277,\n      4271175723,\n      1013904242,\n      1595750129,\n      2773480762,\n      2917565137,\n      1359893119,\n      725511199,\n      2600822924,\n      4215389547,\n      528734635,\n      327033209,\n      1541459225\n    ]);\n    var SIGMA8 = [\n      0,\n      1,\n      2,\n      3,\n      4,\n      5,\n      6,\n      7,\n      8,\n      9,\n      10,\n      11,\n      12,\n      13,\n      14,\n      15,\n      14,\n      10,\n      4,\n      8,\n      9,\n      15,\n      13,\n      6,\n      1,\n      12,\n      0,\n      2,\n      11,\n      7,\n      5,\n      3,\n      11,\n      8,\n      12,\n      0,\n      5,\n      2,\n      15,\n      13,\n      10,\n      14,\n      3,\n      6,\n      7,\n      1,\n      9,\n      4,\n      7,\n      9,\n      3,\n      1,\n      13,\n      12,\n      11,\n      14,\n      2,\n      6,\n      5,\n      10,\n      4,\n      0,\n      15,\n      8,\n      9,\n      0,\n      5,\n      7,\n      2,\n      4,\n      10,\n      15,\n      14,\n      1,\n      11,\n      12,\n      6,\n      8,\n      3,\n      13,\n      2,\n      12,\n      6,\n      10,\n      0,\n      11,\n      8,\n      3,\n      4,\n      13,\n      7,\n      5,\n      15,\n      14,\n      1,\n      9,\n      12,\n      5,\n      1,\n      15,\n      14,\n      13,\n      4,\n      10,\n      0,\n      7,\n      6,\n      3,\n      9,\n      2,\n      8,\n      11,\n      13,\n      11,\n      7,\n      14,\n      12,\n      1,\n      3,\n      9,\n      5,\n      0,\n      15,\n      4,\n      8,\n      6,\n      2,\n      10,\n      6,\n      15,\n      14,\n      9,\n      11,\n      3,\n      0,\n      8,\n      12,\n      2,\n      13,\n      7,\n      1,\n      4,\n      10,\n      5,\n      10,\n      2,\n      8,\n      4,\n      7,\n      6,\n      1,\n      5,\n      15,\n      11,\n      9,\n      14,\n      3,\n      12,\n      13,\n      0,\n      0,\n      1,\n      2,\n      3,\n      4,\n      5,\n      6,\n      7,\n      8,\n      9,\n      10,\n      11,\n      12,\n      13,\n      14,\n      15,\n      14,\n      10,\n      4,\n      8,\n      9,\n      15,\n      13,\n      6,\n      1,\n      12,\n      0,\n      2,\n      11,\n      7,\n      5,\n      3\n    ];\n    var SIGMA82 = new Uint8Array(\n      SIGMA8.map(function(x) {\n        return x * 2;\n      })\n    );\n    var v = new Uint32Array(32);\n    var m = new Uint32Array(32);\n    function blake2bCompress(ctx, last) {\n      let i = 0;\n      for (i = 0; i < 16; i++) {\n        v[i] = ctx.h[i];\n        v[i + 16] = BLAKE2B_IV32[i];\n      }\n      v[24] = v[24] ^ ctx.t;\n      v[25] = v[25] ^ ctx.t / 4294967296;\n      if (last) {\n        v[28] = ~v[28];\n        v[29] = ~v[29];\n      }\n      for (i = 0; i < 32; i++) {\n        m[i] = B2B_GET32(ctx.b, 4 * i);\n      }\n      for (i = 0; i < 12; i++) {\n        B2B_G(0, 8, 16, 24, SIGMA82[i * 16 + 0], SIGMA82[i * 16 + 1]);\n        B2B_G(2, 10, 18, 26, SIGMA82[i * 16 + 2], SIGMA82[i * 16 + 3]);\n        B2B_G(4, 12, 20, 28, SIGMA82[i * 16 + 4], SIGMA82[i * 16 + 5]);\n        B2B_G(6, 14, 22, 30, SIGMA82[i * 16 + 6], SIGMA82[i * 16 + 7]);\n        B2B_G(0, 10, 20, 30, SIGMA82[i * 16 + 8], SIGMA82[i * 16 + 9]);\n        B2B_G(2, 12, 22, 24, SIGMA82[i * 16 + 10], SIGMA82[i * 16 + 11]);\n        B2B_G(4, 14, 16, 26, SIGMA82[i * 16 + 12], SIGMA82[i * 16 + 13]);\n        B2B_G(6, 8, 18, 28, SIGMA82[i * 16 + 14], SIGMA82[i * 16 + 15]);\n      }\n      for (i = 0; i < 16; i++) {\n        ctx.h[i] = ctx.h[i] ^ v[i] ^ v[i + 16];\n      }\n    }\n    var parameterBlock = new Uint8Array([\n      0,\n      0,\n      0,\n      0,\n      //  0: outlen, keylen, fanout, depth\n      0,\n      0,\n      0,\n      0,\n      //  4: leaf length, sequential mode\n      0,\n      0,\n      0,\n      0,\n      //  8: node offset\n      0,\n      0,\n      0,\n      0,\n      // 12: node offset\n      0,\n      0,\n      0,\n      0,\n      // 16: node depth, inner length, rfu\n      0,\n      0,\n      0,\n      0,\n      // 20: rfu\n      0,\n      0,\n      0,\n      0,\n      // 24: rfu\n      0,\n      0,\n      0,\n      0,\n      // 28: rfu\n      0,\n      0,\n      0,\n      0,\n      // 32: salt\n      0,\n      0,\n      0,\n      0,\n      // 36: salt\n      0,\n      0,\n      0,\n      0,\n      // 40: salt\n      0,\n      0,\n      0,\n      0,\n      // 44: salt\n      0,\n      0,\n      0,\n      0,\n      // 48: personal\n      0,\n      0,\n      0,\n      0,\n      // 52: personal\n      0,\n      0,\n      0,\n      0,\n      // 56: personal\n      0,\n      0,\n      0,\n      0\n      // 60: personal\n    ]);\n    function blake2bInit(outlen, key, salt, personal) {\n      if (outlen === 0 || outlen > 64) {\n        throw new Error("Illegal output length, expected 0 < length <= 64");\n      }\n      if (key && key.length > 64) {\n        throw new Error("Illegal key, expected Uint8Array with 0 < length <= 64");\n      }\n      if (salt && salt.length !== 16) {\n        throw new Error("Illegal salt, expected Uint8Array with length is 16");\n      }\n      if (personal && personal.length !== 16) {\n        throw new Error("Illegal personal, expected Uint8Array with length is 16");\n      }\n      const ctx = {\n        b: new Uint8Array(128),\n        h: new Uint32Array(16),\n        t: 0,\n        // input count\n        c: 0,\n        // pointer within buffer\n        outlen\n        // output length in bytes\n      };\n      parameterBlock.fill(0);\n      parameterBlock[0] = outlen;\n      if (key)\n        parameterBlock[1] = key.length;\n      parameterBlock[2] = 1;\n      parameterBlock[3] = 1;\n      if (salt)\n        parameterBlock.set(salt, 32);\n      if (personal)\n        parameterBlock.set(personal, 48);\n      for (let i = 0; i < 16; i++) {\n        ctx.h[i] = BLAKE2B_IV32[i] ^ B2B_GET32(parameterBlock, i * 4);\n      }\n      if (key) {\n        blake2bUpdate(ctx, key);\n        ctx.c = 128;\n      }\n      return ctx;\n    }\n    function blake2bUpdate(ctx, input) {\n      for (let i = 0; i < input.length; i++) {\n        if (ctx.c === 128) {\n          ctx.t += ctx.c;\n          blake2bCompress(ctx, false);\n          ctx.c = 0;\n        }\n        ctx.b[ctx.c++] = input[i];\n      }\n    }\n    function blake2bFinal(ctx) {\n      ctx.t += ctx.c;\n      while (ctx.c < 128) {\n        ctx.b[ctx.c++] = 0;\n      }\n      blake2bCompress(ctx, true);\n      const out = new Uint8Array(ctx.outlen);\n      for (let i = 0; i < ctx.outlen; i++) {\n        out[i] = ctx.h[i >> 2] >> 8 * (i & 3);\n      }\n      return out;\n    }\n    function blake2b2(input, key, outlen, salt, personal) {\n      outlen = outlen || 64;\n      input = util.normalizeInput(input);\n      if (salt) {\n        salt = util.normalizeInput(salt);\n      }\n      if (personal) {\n        personal = util.normalizeInput(personal);\n      }\n      const ctx = blake2bInit(outlen, key, salt, personal);\n      blake2bUpdate(ctx, input);\n      return blake2bFinal(ctx);\n    }\n    function blake2bHex(input, key, outlen, salt, personal) {\n      const output = blake2b2(input, key, outlen, salt, personal);\n      return util.toHex(output);\n    }\n    module.exports = {\n      blake2b: blake2b2,\n      blake2bHex,\n      blake2bInit,\n      blake2bUpdate,\n      blake2bFinal\n    };\n  }\n});\n\n// node_modules/blakejs/blake2s.js\nvar require_blake2s = __commonJS({\n  "node_modules/blakejs/blake2s.js"(exports, module) {\n    var util = require_util();\n    function B2S_GET32(v2, i) {\n      return v2[i] ^ v2[i + 1] << 8 ^ v2[i + 2] << 16 ^ v2[i + 3] << 24;\n    }\n    function B2S_G(a, b, c, d, x, y) {\n      v[a] = v[a] + v[b] + x;\n      v[d] = ROTR32(v[d] ^ v[a], 16);\n      v[c] = v[c] + v[d];\n      v[b] = ROTR32(v[b] ^ v[c], 12);\n      v[a] = v[a] + v[b] + y;\n      v[d] = ROTR32(v[d] ^ v[a], 8);\n      v[c] = v[c] + v[d];\n      v[b] = ROTR32(v[b] ^ v[c], 7);\n    }\n    function ROTR32(x, y) {\n      return x >>> y ^ x << 32 - y;\n    }\n    var BLAKE2S_IV = new Uint32Array([\n      1779033703,\n      3144134277,\n      1013904242,\n      2773480762,\n      1359893119,\n      2600822924,\n      528734635,\n      1541459225\n    ]);\n    var SIGMA = new Uint8Array([\n      0,\n      1,\n      2,\n      3,\n      4,\n      5,\n      6,\n      7,\n      8,\n      9,\n      10,\n      11,\n      12,\n      13,\n      14,\n      15,\n      14,\n      10,\n      4,\n      8,\n      9,\n      15,\n      13,\n      6,\n      1,\n      12,\n      0,\n      2,\n      11,\n      7,\n      5,\n      3,\n      11,\n      8,\n      12,\n      0,\n      5,\n      2,\n      15,\n      13,\n      10,\n      14,\n      3,\n      6,\n      7,\n      1,\n      9,\n      4,\n      7,\n      9,\n      3,\n      1,\n      13,\n      12,\n      11,\n      14,\n      2,\n      6,\n      5,\n      10,\n      4,\n      0,\n      15,\n      8,\n      9,\n      0,\n      5,\n      7,\n      2,\n      4,\n      10,\n      15,\n      14,\n      1,\n      11,\n      12,\n      6,\n      8,\n      3,\n      13,\n      2,\n      12,\n      6,\n      10,\n      0,\n      11,\n      8,\n      3,\n      4,\n      13,\n      7,\n      5,\n      15,\n      14,\n      1,\n      9,\n      12,\n      5,\n      1,\n      15,\n      14,\n      13,\n      4,\n      10,\n      0,\n      7,\n      6,\n      3,\n      9,\n      2,\n      8,\n      11,\n      13,\n      11,\n      7,\n      14,\n      12,\n      1,\n      3,\n      9,\n      5,\n      0,\n      15,\n      4,\n      8,\n      6,\n      2,\n      10,\n      6,\n      15,\n      14,\n      9,\n      11,\n      3,\n      0,\n      8,\n      12,\n      2,\n      13,\n      7,\n      1,\n      4,\n      10,\n      5,\n      10,\n      2,\n      8,\n      4,\n      7,\n      6,\n      1,\n      5,\n      15,\n      11,\n      9,\n      14,\n      3,\n      12,\n      13,\n      0\n    ]);\n    var v = new Uint32Array(16);\n    var m = new Uint32Array(16);\n    function blake2sCompress(ctx, last) {\n      let i = 0;\n      for (i = 0; i < 8; i++) {\n        v[i] = ctx.h[i];\n        v[i + 8] = BLAKE2S_IV[i];\n      }\n      v[12] ^= ctx.t;\n      v[13] ^= ctx.t / 4294967296;\n      if (last) {\n        v[14] = ~v[14];\n      }\n      for (i = 0; i < 16; i++) {\n        m[i] = B2S_GET32(ctx.b, 4 * i);\n      }\n      for (i = 0; i < 10; i++) {\n        B2S_G(0, 4, 8, 12, m[SIGMA[i * 16 + 0]], m[SIGMA[i * 16 + 1]]);\n        B2S_G(1, 5, 9, 13, m[SIGMA[i * 16 + 2]], m[SIGMA[i * 16 + 3]]);\n        B2S_G(2, 6, 10, 14, m[SIGMA[i * 16 + 4]], m[SIGMA[i * 16 + 5]]);\n        B2S_G(3, 7, 11, 15, m[SIGMA[i * 16 + 6]], m[SIGMA[i * 16 + 7]]);\n        B2S_G(0, 5, 10, 15, m[SIGMA[i * 16 + 8]], m[SIGMA[i * 16 + 9]]);\n        B2S_G(1, 6, 11, 12, m[SIGMA[i * 16 + 10]], m[SIGMA[i * 16 + 11]]);\n        B2S_G(2, 7, 8, 13, m[SIGMA[i * 16 + 12]], m[SIGMA[i * 16 + 13]]);\n        B2S_G(3, 4, 9, 14, m[SIGMA[i * 16 + 14]], m[SIGMA[i * 16 + 15]]);\n      }\n      for (i = 0; i < 8; i++) {\n        ctx.h[i] ^= v[i] ^ v[i + 8];\n      }\n    }\n    function blake2sInit(outlen, key) {\n      if (!(outlen > 0 && outlen <= 32)) {\n        throw new Error("Incorrect output length, should be in [1, 32]");\n      }\n      const keylen = key ? key.length : 0;\n      if (key && !(keylen > 0 && keylen <= 32)) {\n        throw new Error("Incorrect key length, should be in [1, 32]");\n      }\n      const ctx = {\n        h: new Uint32Array(BLAKE2S_IV),\n        // hash state\n        b: new Uint8Array(64),\n        // input block\n        c: 0,\n        // pointer within block\n        t: 0,\n        // input count\n        outlen\n        // output length in bytes\n      };\n      ctx.h[0] ^= 16842752 ^ keylen << 8 ^ outlen;\n      if (keylen > 0) {\n        blake2sUpdate(ctx, key);\n        ctx.c = 64;\n      }\n      return ctx;\n    }\n    function blake2sUpdate(ctx, input) {\n      for (let i = 0; i < input.length; i++) {\n        if (ctx.c === 64) {\n          ctx.t += ctx.c;\n          blake2sCompress(ctx, false);\n          ctx.c = 0;\n        }\n        ctx.b[ctx.c++] = input[i];\n      }\n    }\n    function blake2sFinal(ctx) {\n      ctx.t += ctx.c;\n      while (ctx.c < 64) {\n        ctx.b[ctx.c++] = 0;\n      }\n      blake2sCompress(ctx, true);\n      const out = new Uint8Array(ctx.outlen);\n      for (let i = 0; i < ctx.outlen; i++) {\n        out[i] = ctx.h[i >> 2] >> 8 * (i & 3) & 255;\n      }\n      return out;\n    }\n    function blake2s(input, key, outlen) {\n      outlen = outlen || 32;\n      input = util.normalizeInput(input);\n      const ctx = blake2sInit(outlen, key);\n      blake2sUpdate(ctx, input);\n      return blake2sFinal(ctx);\n    }\n    function blake2sHex(input, key, outlen) {\n      const output = blake2s(input, key, outlen);\n      return util.toHex(output);\n    }\n    module.exports = {\n      blake2s,\n      blake2sHex,\n      blake2sInit,\n      blake2sUpdate,\n      blake2sFinal\n    };\n  }\n});\n\n// node_modules/blakejs/index.js\nvar require_blakejs = __commonJS({\n  "node_modules/blakejs/index.js"(exports, module) {\n    var b2b = require_blake2b2();\n    var b2s = require_blake2s();\n    module.exports = {\n      blake2b: b2b.blake2b,\n      blake2bHex: b2b.blake2bHex,\n      blake2bInit: b2b.blake2bInit,\n      blake2bUpdate: b2b.blake2bUpdate,\n      blake2bFinal: b2b.blake2bFinal,\n      blake2s: b2s.blake2s,\n      blake2sHex: b2s.blake2sHex,\n      blake2sInit: b2s.blake2sInit,\n      blake2sUpdate: b2s.blake2sUpdate,\n      blake2sFinal: b2s.blake2sFinal\n    };\n  }\n});\n\n// js/blake2-wrapper.js\nvar require_blake2_wrapper = __commonJS({\n  "js/blake2-wrapper.js"(exports, module) {\n    var b2wasm = __toESM(require_blake2b_wasm());\n    var import_blakejs = __toESM(require_blakejs());\n    function Blake2b(outlen, key, salt, personal) {\n      return (0, import_blakejs.blake2bInit)(outlen, key);\n    }\n    Blake2b.prototype.update = function(input) {\n      (0, import_blakejs.blake2bUpdate)(this, input);\n      return this;\n    };\n    Blake2b.prototype.digest = function(out) {\n      var buf = !out ? new Uint8Array(this.outlen) : out;\n      (0, import_blakejs.blake2bFinal)(this, buf);\n      return buf;\n    };\n    Blake2b.prototype.final = Blake2b.prototype.digest;\n    Blake2b.ready = function(cb) {\n      b2wasm.ready(function() {\n        cb();\n      });\n    };\n    module.exports.impl = function createHash(outlen, key, salt, personal) {\n      return new Blake2b(outlen, key);\n    };\n    module.exports.ready = function(cb) {\n      b2wasm.ready(function() {\n        cb();\n      });\n    };\n    module.exports.WASM_SUPPORTED = b2wasm.SUPPORTED;\n    module.exports.WASM_LOADED = false;\n    b2wasm.ready(function(err) {\n      if (!err) {\n        module.exports.WASM_LOADED = true;\n        module.exports.impl = b2wasm.default;\n      }\n    });\n  }\n});\n\n// js/puzzle.worker.js\nvar import_blake2_wrapper = __toESM(require_blake2_wrapper());\nvar blake2b = import_blake2_wrapper.default.impl;\nvar blake2bInitialized = false;\nvar puzzleBuffer = null;\nvar puzzleID = null;\nif (import_blake2_wrapper.default.ready) {\n  import_blake2_wrapper.default.ready(() => {\n    console.debug("[privatecaptcha][worker] Blake2b loaded. Wasm: " + import_blake2_wrapper.default.WASM_LOADED);\n    blake2b = import_blake2_wrapper.default.impl;\n    blake2bInitialized = true;\n    if (puzzleBuffer) {\n      self.postMessage({ command: "init" });\n    }\n  });\n}\nfunction readUInt32LE(buffer, offset) {\n  return (buffer[offset] | buffer[offset + 1] << 8 | buffer[offset + 2] << 16 | buffer[offset + 3] << 24) >>> 0;\n}\nfunction thresholdFromDifficulty(d) {\n  return Math.pow(2, Math.floor((255.999 - d) / 8)) >>> 0;\n}\nfunction findSolution(threshold, puzzleIndex, debug) {\n  const length = puzzleBuffer.length;\n  if (debug) {\n    console.debug(`[privatecaptcha][worker] looking for a solution. threshold=${threshold} puzzleID=${puzzleIndex} length=${length}`);\n  }\n  puzzleBuffer[length - 8] = puzzleIndex;\n  let hash = new Uint8Array(32);\n  for (let i = 0; i < 256; i++) {\n    puzzleBuffer[length - 1 - 3] = i;\n    for (let j = 0; j < 256; j++) {\n      puzzleBuffer[length - 1 - 2] = j;\n      for (let k = 0; k < 256; k++) {\n        puzzleBuffer[length - 1 - 1] = j;\n        for (let l = 0; l < 256; l++) {\n          puzzleBuffer[length - 1 - 0] = l;\n          hash.fill(0);\n          blake2b(hash.length).update(puzzleBuffer).digest(hash);\n          const prefix = readUInt32LE(hash, 0);\n          if (prefix <= threshold) {\n            if (debug) {\n              console.debug(`[privatecaptcha][worker] found solution. prefix=${prefix} threshold=${threshold}`);\n            }\n            return puzzleBuffer.subarray(length - 8);\n          }\n        }\n      }\n    }\n  }\n  return new Uint8Array(0);\n}\nself.onmessage = (event) => {\n  const { command, argument } = event.data;\n  switch (command) {\n    case "init":\n      const { id, buffer } = argument;\n      puzzleID = id;\n      puzzleBuffer = buffer;\n      if (blake2bInitialized) {\n        self.postMessage({ command: "init" });\n      }\n      break;\n    case "solve":\n      const { difficulty, puzzleIndex, debug } = argument;\n      const threshold = thresholdFromDifficulty(difficulty);\n      const solution = findSolution(threshold, puzzleIndex, debug);\n      self.postMessage({ command, argument: { id: puzzleID, solution } });\n      break;\n    default:\n      break;\n  }\n};\n');
  }

  // js/workerspool.js
  var WorkersPool = class {
    constructor(callbacks = {}, debug = false) {
      this._solutions = [];
      this._solutionsCount = 0;
      this._puzzleID = null;
      this._workers = [];
      this._debug = debug;
      this._callbacks = Object.assign({
        workersReady: () => 0,
        workerError: () => 0,
        workCompleted: () => 0,
        progress: () => 0
      }, callbacks);
    }
    init(puzzleID, puzzleData, autoStart) {
      const workersCount = 4;
      let readyWorkers = 0;
      const workers = [];
      const pool = this;
      for (let i = 0; i < workersCount; i++) {
        const worker = new Worker2();
        worker.onerror = (e) => this._callbacks.workerError(e);
        worker.onmessage = function(event) {
          if (!event.data) {
            return;
          }
          const command = event.data.command;
          switch (command) {
            case "init":
              readyWorkers++;
              if (readyWorkers === workersCount) {
                pool._callbacks.workersReady(autoStart);
              }
              break;
            case "solve":
              const { id, solution } = event.data.argument;
              pool.onSolutionFound(id, solution);
              break;
            case "error":
              if (event.data.error) {
                pool._callbacks.workerError(event.data.error);
              }
              break;
            default:
              break;
          }
          ;
        };
        workers.push(worker);
      }
      this._workers = workers;
      this._puzzleID = puzzleID;
      if (this._debug) {
        console.debug(`[privatecaptcha][pool] initializing workers. count=${this._workers.length}`);
      }
      for (let i = 0; i < this._workers.length; i++) {
        this._workers[i].postMessage({
          command: "init",
          argument: {
            id: puzzleID,
            buffer: puzzleData
          }
        });
      }
      ;
    }
    solve(puzzle) {
      if (!puzzle) {
        return;
      }
      if (this._debug) {
        console.debug("[privatecaptcha][pool] starting solving");
      }
      this._solutions = [];
      this._solutionsCount = puzzle.solutionsCount;
      this._puzzleID = puzzle.ID;
      for (let i = 0; i < puzzle.solutionsCount; i++) {
        this._workers[i % this._workers.length].postMessage({
          command: "solve",
          argument: {
            difficulty: puzzle.difficulty,
            puzzleIndex: i,
            debug: this._debug
          }
        });
      }
    }
    stop() {
      const count = this._workers.length;
      for (let i = 0; i < count; i++) {
        this._workers[i].terminate();
      }
      this._workers = [];
      if (this._debug) {
        console.debug("[privatecaptcha][pool] terminated the workers. count=" + count);
      }
    }
    onSolutionFound(id, solution) {
      if (this._debug) {
        console.debug("[privatecaptcha][pool] solution found. length=" + solution.length);
      }
      if (id != this._puzzleID) {
        console.warn(`[privatecaptcha][pool] Discarding solution with invalid ID. actual=${id} expected=${this._puzzleID}`);
        return;
      }
      this._solutions.push(solution);
      const count = this._solutions.length;
      this._callbacks.progress(count * 100 / this._solutionsCount);
      if (count == this._solutionsCount) {
        this._callbacks.workCompleted();
      }
    }
    serializeSolutions() {
      if (this._debug) {
        console.debug("[privatecaptcha][pool] solutions found. count=" + this._solutions.length);
      }
      const totalLength = this._solutions.reduce((total, arr) => total + arr.length, 0);
      const resultArray = new Uint8Array(totalLength);
      let offset = 0;
      for (let i = 0; i < this._solutions.length; i++) {
        resultArray.set(this._solutions[i], offset);
        offset += this._solutions[i].length;
      }
      return encode(resultArray);
    }
  };

  // js/progress.js
  var ProgressRing = class extends HTMLElement {
    constructor() {
      super();
      const stroke = this.getAttribute("stroke");
      const attrSize = this.getAttribute("size");
      const size = 100;
      const radius = size / 2;
      const normalizedRadius = radius - stroke / 2;
      this._circumference = normalizedRadius * 2 * Math.PI;
      this._root = this.attachShadow({ mode: "open" });
      this._root.innerHTML = `
          <svg height="${attrSize}" width="${attrSize}" preserveAspectRatio="xMidYMid meet" viewBox="0 0 ${size} ${size}">
            <circle
               id="pie"
               style="stroke-dasharray: 0 ${this._circumference}"
               stroke-width="${normalizedRadius}"
               fill="transparent"
               r="${normalizedRadius / 2}"
               cx="${radius}"
               cy="${radius}"
            />
            <circle
               id="track"
               stroke-width="${stroke}"
               fill="transparent"
               r="${normalizedRadius}"
               cx="${radius}"
               cy="${radius}"
            />
             <circle
               id="progress"
               stroke-dasharray="${this._circumference} ${this._circumference}"
               style="stroke-dashoffset: ${this._circumference}"
               stroke-width="${stroke}"
               fill="transparent"
               r="${normalizedRadius}"
               cx="${radius}"
               cy="${radius}"
            />
          </svg>

          <style>
            #pie {
                stroke: var(--light-color);
                transition: stroke-dasharray 0.35s;
                transform: rotate(-90deg);
                transform-origin: 50% 50%;
            }
            #progress {
                stroke: var(--accent-color);
                transition: stroke-dashoffset 0.35s;
                transform: rotate(-90deg);
                transform-origin: 50% 50%;
            }
            #track {
                stroke: var(--gray-color);
            }
          </style>
        `;
    }
    setProgress(percent) {
      const progress = percent / 100 * this._circumference;
      const offset = this._circumference - progress;
      const circle = this._root.getElementById("progress");
      circle.style.strokeDashoffset = offset;
      const pie = this._root.getElementById("pie");
      pie.style.strokeDasharray = progress / 2 + " " + this._circumference;
    }
    static get observedAttributes() {
      return ["progress"];
    }
    attributeChangedCallback(name, oldValue, newValue) {
      if (name === "progress") {
        this.setProgress(newValue);
      }
    }
  };

  // js/styles.css
  var styles_default = ":host{--dark-color: #070113;--accent-color: #0080A0;--gray-color: #E0E0E0;--light-color: #F2F2F2;--lighter-color: #FFF;--extra-spacing: 2px;--label-spacing: 10px;--border-radius: 2px}.pc-captcha-widget{display:inline-flex;align-items:center;max-width:360px;padding:12px 16px;border:1px solid var(--gray-color);border-radius:var(--border-radius);gap:64px}.pc-captcha-widget.hidden{display:none}.pc-captcha-widget.floating{position:absolute;left:0;top:0;transform:translateY(-100%);background-color:#fff}.pc-interactive-area{display:flex;align-items:center;position:relative;min-width:160px;font-size:1em}.pc-interactive-area label{padding-left:var(--label-spacing);color:var(--dark-color)}.pc-interactive-area input[type=checkbox]{width:32px;height:32px;margin:0 0 0 var(--extra-spacing);appearance:none;background-color:var(--lighter-color);border:2px solid var(--dark-color);border-radius:4px;cursor:pointer}.pc-interactive-area input[type=checkbox]+label{padding:0 0 0 calc(var(--extra-spacing) + var(--label-spacing))}@keyframes colorChange{0%,to{border-color:var(--dark-color)}50%{border-color:var(--gray-color)}}.pc-interactive-area input[type=checkbox].loading{animation:colorChange 2s infinite;animation-timing-function:ease-in-out;background-color:var(--light-color)}.pc-interactive-area input[type=checkbox].ready{background-color:var(--lighter-color);border-color:var(--dark-color)}.pc-interactive-area input[type=checkbox]:hover{background-color:var(--lighter-color);border-color:var(--accent-color)}.pc-interactive-area input[type=checkbox]:hover+label{cursor:pointer}@keyframes dots-1{0%{opacity:0}25%{opacity:1}}@keyframes dots-2{0%{opacity:0}50%{opacity:1}}@keyframes dots-3{0%{opacity:0}75%{opacity:1}}@-webkit-keyframes dots-1{0%{opacity:0}25%{opacity:1}}@-webkit-keyframes dots-2{0%{opacity:0}50%{opacity:1}}@-webkit-keyframes dots-3{0%{opacity:0}75%{opacity:1}}.pc-interactive-area .dots span{animation:dots-1 2s infinite steps(1);-webkit-animation:dots-1 2s infinite steps(1)}.pc-interactive-area .dots span:first-child+span{animation-name:dots-2;-webkit-animation-name:dots-2}.pc-interactive-area .dots span:first-child+span+span{animation-name:dots-3;-webkit-animation-name:dots-3}#pc-progress{display:flex;justify-content:center}.pc-info{display:flex;flex-direction:column;align-items:center;margin-right:var(--extra-spacing);color:var(--dark-color)}.company-logo{max-width:100px;height:auto}a.pc-link{margin-top:5px;text-decoration:none;color:currentcolor;text-align:center;font-size:.55em;text-transform:uppercase;font-weight:700;line-height:1.1em}a.pc-link:hover{text-decoration:underline}@keyframes checkmark{0%{stroke-dashoffset:100px}to{stroke-dashoffset:0px}}svg.verified polyline{animation:checkmark .35s ease-in-out .1s backwards}#pc-debug{font-size:12px;color:var(--gray-color);position:absolute;top:100%;left:50px}\n";

  // js/strings.js
  var CLICK_TO_VERIFY = "click_to_verify";
  var VERIFYING = "verifying";
  var SUCCESS = "success";
  var STRINGS = {
    "en": {
      [CLICK_TO_VERIFY]: "Click to verify",
      [VERIFYING]: "Verifying",
      [SUCCESS]: "Success"
    }
  };

  // js/html.js
  window.customElements.define("progress-ring", ProgressRing);
  var STATE_EMPTY = "empty";
  var STATE_ERROR = "error";
  var STATE_LOADING = "loading";
  var STATE_READY = "ready";
  var STATE_IN_PROGRESS = "inprogress";
  var STATE_VERIFIED = "verified";
  var DISPLAY_POPUP = "popup";
  var DISPLAY_HIDDEN = "hidden";
  var DISPLAY_WIDGET = "widget";
  var RING_SIZE = 36;
  var CHECKBOX_ID = "pc-checkbox";
  var PROGRESS_ID = "pc-progress";
  var DEBUG_ID = "pc-debug";
  var privateCaptchaSVG = `<svg viewBox="0 0 39.4 41.99" xml:space="preserve" width="39.4" height="41.99" xmlns="http://www.w3.org/2000/svg" class="pc-logo">
<path d="M0 0v30.62l4.29 2.48V4.85h30.83v23.29l-15.41 8.9-6.83-3.94v-4.95l6.83 3.94 11.12-6.42V9.91H8.58v25.66l11.12 6.42 19.7-11.37V0Zm12.87 14.86h13.66v8.32l-6.83 3.94-6.83-3.94z" fill="currentColor"/>
</svg>`;
  var verifiedSVG = `<svg class="verified" xmlns="http://www.w3.org/2000/svg" width="${RING_SIZE}px" height="${RING_SIZE}px" viewBox="0 0 154 154">
<g fill="none"><circle fill="#0080A0" cx="77" cy="77" r="76"></circle>
<polyline class="st0" stroke="#F2F2F2" stroke-width="12" points="43.5,77.8 63.7,97.9 112.2,49.4" style="stroke-dasharray:100px, 100px; stroke-dashoffset: 200px;"/></g>
</svg>
`;
  var activeAreaEmptyCheckbox = `<input type="checkbox" id="${CHECKBOX_ID}" required>`;
  function checkbox(cls) {
    return `<input type="checkbox" id="${CHECKBOX_ID}" class="${cls}" required>`;
  }
  function label(text, forElement) {
    return `<label for="${forElement}">${text}</label>`;
  }
  var CaptchaElement = class extends HTMLElement {
    constructor() {
      super();
      this._state = "";
      this._root = this.attachShadow({ mode: "open" });
      this._debug = this.getAttribute("debug");
      this._displayMode = this.getAttribute("display-mode");
      this._lang = this.getAttribute("lang");
      if (!(this._lang in STRINGS)) {
        console.warn(`[privatecaptcha][progress] Localization not found. lang=${this._lang}`);
        this._lang = "en";
      }
      this.checkEvent = new CustomEvent("check", {
        bubbles: true,
        cancelable: false,
        composed: true
      });
      const sheet = new CSSStyleSheet();
      sheet.replace(styles_default);
      this._root.adoptedStyleSheets.push(sheet);
      const extraStyles = this.getAttribute("extra-styles");
      if (extraStyles) {
        const overridesSheet = new CSSStyleSheet();
        overridesSheet.replace(extraStyles);
        this._root.adoptedStyleSheets.push(overridesSheet);
      }
      this.setState(STATE_EMPTY);
    }
    setState(state) {
      if (state == this._state) {
        console.debug("[privatecaptcha][progress] already in this state: " + state);
        return;
      }
      if (this._debug) {
        console.debug(`[privatecaptcha][progress] change state old=${this._state} new=${state}`);
      }
      let activeArea = "";
      let bindCheckEvent = false;
      let showPopupIfNeeded = false;
      const strings = STRINGS[this._lang];
      switch (state) {
        case STATE_EMPTY:
          bindCheckEvent = true;
          activeArea = activeAreaEmptyCheckbox + label(strings[CLICK_TO_VERIFY], CHECKBOX_ID);
          break;
        case STATE_LOADING:
          bindCheckEvent = true;
          activeArea = checkbox("loading") + label(strings[CLICK_TO_VERIFY], CHECKBOX_ID);
          break;
        case STATE_READY:
          bindCheckEvent = true;
          activeArea = checkbox("ready") + label(strings[CLICK_TO_VERIFY], CHECKBOX_ID);
          showPopupIfNeeded = true;
          break;
        case STATE_IN_PROGRESS:
          const text = strings[VERIFYING];
          activeArea = `<progress-ring id="${PROGRESS_ID}" stroke="12" size="${RING_SIZE}" progress="0"></progress-ring><label for="${PROGRESS_ID}">${text}<span class="dots"><span>.</span><span>.</span><span>.</span></span></label>`;
          showPopupIfNeeded = true;
          break;
        case STATE_VERIFIED:
          activeArea = verifiedSVG + label(strings[SUCCESS], PROGRESS_ID);
          showPopupIfNeeded = true;
          break;
        default:
          console.error(`[privatecaptcha][progress] unknown state: ${state}`);
          break;
      }
      if (this._debug) {
        activeArea += `<span id="${DEBUG_ID}">[${state}]</span>`;
      }
      let displayClass = "";
      switch (this._displayMode) {
        case DISPLAY_HIDDEN:
          displayClass = "hidden";
          break;
        case DISPLAY_POPUP:
          displayClass = showPopupIfNeeded ? "floating" : "hidden";
          break;
        case DISPLAY_WIDGET:
          break;
      }
      ;
      this._state = state;
      this._root.innerHTML = `<div class="pc-captcha-widget ${displayClass}">
            <div class="pc-interactive-area">
                ${activeArea}
            </div>
            <div class="pc-info">
                ${privateCaptchaSVG}
                <a href="https://privatecaptcha.com" class="pc-link" rel="noopener" target="_blank">Private<br />Captcha</a>
            </div>
        </div>`;
      if (bindCheckEvent) {
        const checkbox2 = this._root.getElementById(CHECKBOX_ID);
        if (checkbox2) {
          checkbox2.addEventListener("change", this.onCheckboxClicked.bind(this));
        } else {
          console.warn("[privatecaptcha][progress] checkbox not found in the Shadow DOM");
        }
      }
    }
    onCheckboxClicked(event) {
      event.preventDefault();
      if (this._debug) {
        console.debug("[privatecaptcha][progress] checkbox was clicked");
      }
      if (event.target.checked) {
        this.dispatchEvent(this.checkEvent);
      } else {
        console.warn("[privatecaptcha][progress] checkbox was unchecked");
      }
    }
    setProgress(percent) {
      if (STATE_IN_PROGRESS == this._state) {
        const progressBar = this._root.getElementById(PROGRESS_ID);
        if (progressBar) {
          progressBar.setProgress(percent);
        } else {
          console.warn("[privatecaptcha][progress] progress element not found");
        }
      }
    }
    setDebugState(state) {
      const debugElement = this._root.getElementById(DEBUG_ID);
      if (debugElement) {
        debugElement.innerHTML = `[${state}]`;
      }
    }
    static get observedAttributes() {
      return ["state", "progress"];
    }
    attributeChangedCallback(name, oldValue, newValue) {
      if ("progress" === name) {
        this.setProgress(newValue);
      }
    }
  };

  // js/widget.js
  window.customElements.define("private-captcha", CaptchaElement);
  var PUZZLE_ENDPOINT_URL = "/api/puzzle";
  function findParentFormElement(element) {
    while (element && element.tagName !== "FORM") {
      element = element.parentElement;
    }
    return element;
  }
  var CaptchaWidget = class {
    constructor(element, options = {}) {
      this._element = element;
      this._puzzle = null;
      this._expiryTimeout = null;
      this._state = STATE_EMPTY;
      this._lastProgress = null;
      this._userStarted = false;
      this._options = {};
      this.setOptions(options);
      this._workersPool = new WorkersPool({
        workersReady: this.onWorkersReady.bind(this),
        workerError: this.onWorkerError.bind(this),
        workCompleted: this.onWorkCompleted.bind(this),
        progress: this.onWorkProgress.bind(this)
      }, this._options.debug);
      const form = findParentFormElement(this._element);
      if (form) {
        form.addEventListener("focusin", this.onFocusIn.bind(this), { once: true, passive: true });
        this._element.innerHTML = `<private-captcha display-mode="${this._options.displayMode}" lang="${this._options.lang}" extra-styles="${this._options.styles}"${this._options.debug ? ' debug="true"' : ""}></private-captcha>`;
        this._element.addEventListener("check", this.onChecked.bind(this));
        if (DISPLAY_POPUP === this._options.displayMode) {
          const anchor = form.querySelector(".private-captcha-anchor");
          if (anchor) {
            anchor.style.position = "relative";
          } else {
            console.warn("[privatecaptcha] cannot find anchor for popup");
          }
        }
      } else {
        console.warn("[privatecaptcha] cannot find form element");
      }
    }
    setOptions(options) {
      this._options = Object.assign({
        startMode: this._element.dataset["startMode"] || "click",
        debug: this._element.dataset["debug"],
        fieldName: this._element.dataset["solutionField"] || "private-captcha-solution",
        puzzleEndpoint: this._element.dataset["puzzleEndpoint"] || PUZZLE_ENDPOINT_URL,
        sitekey: this._element.dataset["sitekey"] || "",
        displayMode: this._element.dataset["displayMode"] || "widget",
        lang: this._element.dataset["lang"] || "en",
        styles: this._element.dataset["styles"] || ""
      }, options);
    }
    // fetches puzzle from the server and setup workers
    async init(autoStart) {
      this.trace("init() was called");
      const sitekey = this._options.sitekey || this._element.dataset["sitekey"];
      if (!sitekey) {
        console.error("[privatecaptcha] sitekey not set on captcha element");
        return;
      }
      if (this._state != STATE_EMPTY && this._state != STATE_ERROR) {
        console.warn(`[privatecaptcha] captcha has already been initialized. state=${this._state}`);
        return;
      }
      if (this._workersPool) {
        this._workersPool.stop();
      }
      const startWorkers = this._options.startMode == "auto" || autoStart;
      try {
        this.setState(STATE_LOADING);
        this.trace("fetching puzzle");
        const puzzleData = await getPuzzle(this._options.puzzleEndpoint, sitekey);
        this._puzzle = new Puzzle(puzzleData);
        const expirationMillis = this._puzzle.expirationMillis();
        this.trace(`parsed puzzle buffer. ttl=${expirationMillis / 1e3}`);
        if (this._expiryTimeout) {
          clearTimeout(this._expiryTimeout);
        }
        this._expiryTimeout = setTimeout(() => this.expire(), expirationMillis);
        this._workersPool.init(this._puzzle.ID, this._puzzle.puzzleBuffer, startWorkers);
      } catch (e) {
        console.error("[privatecaptcha]", e);
        if (this._expiryTimeout) {
          clearTimeout(this._expiryTimeout);
        }
        this.setState(STATE_ERROR);
        this.setProgressState(this._userStarted ? STATE_VERIFIED : STATE_EMPTY);
        if (this._userStarted) {
          this.signalErrored();
        }
      }
    }
    start() {
      if (this._state !== STATE_READY) {
        console.warn(`[privatecaptcha] solving has already been started. state=${this._state}`);
        return;
      }
      this.trace("starting solving captcha");
      try {
        this.setState(STATE_IN_PROGRESS);
        this._workersPool.solve(this._puzzle);
        this.signalStarted();
      } catch (e) {
        console.error("[privatecaptcha]", e);
      }
    }
    signalStarted() {
      const callback = this._element.dataset["startedCallback"];
      if (callback) {
        window[callback]();
      }
    }
    signalFinished() {
      const callback = this._element.dataset["finishedCallback"];
      if (callback) {
        window[callback]();
      }
    }
    signalErrored() {
      const callback = this._element.dataset["erroredCallback"];
      if (callback) {
        window[callback]();
      }
    }
    ensureNoSolutionField() {
      const solutionField = this._element.querySelector(`input[name="${this._options.fieldName}"]`);
      if (solutionField) {
        try {
          this._element.removeChild(solutionField);
        } catch (e) {
          console.warn("[privatecaptcha]", e);
        }
      }
    }
    reset(options = {}) {
      this.trace("reset captcha");
      if (this._workersPool) {
        this._workersPool.stop();
      }
      if (this._expiryTimeout) {
        clearTimeout(this._expiryTimeout);
      }
      this.setState(STATE_EMPTY);
      this.setProgressState(STATE_EMPTY);
      this.ensureNoSolutionField();
      this._userStarted = false;
      this.setOptions(options);
      this.init(
        false
        /*start*/
      );
    }
    expire() {
      this.trace("time to expire captcha");
      this.setState(STATE_EMPTY);
      this.setProgressState(STATE_EMPTY);
      this.ensureNoSolutionField();
      this.init(this._userStarted);
    }
    onFocusIn(event) {
      this.trace("onFocusIn event handler");
      const pcElement = this._element.querySelector("private-captcha");
      if (pcElement && event.target == pcElement) {
        this.trace("skipping focusin event on captcha element");
        return;
      }
      this.init(
        false
        /*start*/
      );
      this.setProgressState(this._state);
    }
    onChecked() {
      this.trace(`onChecked event handler. state=${this._state}`);
      this._userStarted = true;
      let progressState = STATE_IN_PROGRESS;
      let signal = false;
      switch (this._state) {
        case STATE_READY:
          this.start();
          break;
        case STATE_EMPTY:
        case STATE_ERROR:
          this.init(
            true
            /*start*/
          );
          break;
        case STATE_LOADING:
          break;
        case STATE_IN_PROGRESS:
          setTimeout(() => this.setProgress(this._lastProgress), 500);
          break;
        case STATE_VERIFIED:
          progressState = STATE_VERIFIED;
          signal = true;
          break;
        default:
          console.warn("[privatecaptcha] onChecked: unexpected state. state=" + this._state);
      }
      ;
      this.setProgressState(progressState);
      if (signal) {
        this.signalFinished();
      }
    }
    onWorkersReady(autoStart) {
      this.trace(`workers are ready. autostart=${autoStart}`);
      this.setState(STATE_READY);
      if (!this._userStarted) {
        this.setProgressState(STATE_READY);
      }
      if (autoStart || this._userStarted) {
        this.start();
      }
    }
    onWorkerError(error) {
      console.error("[privatecaptcha] error in worker:", error);
    }
    onWorkCompleted() {
      if (this._state !== STATE_IN_PROGRESS) {
        console.warn(`[privatecaptcha] solving has not been started. state=${this._state}`);
        return;
      }
      this.setState(STATE_VERIFIED);
      if (this._userStarted) {
        this.setProgressState(STATE_VERIFIED);
      }
      const solutions = this._workersPool.serializeSolutions();
      const payload = `${solutions}.${this._puzzle.rawData}`;
      this.trace(`work completed. payload=${payload}`);
      this.ensureNoSolutionField();
      this._element.insertAdjacentHTML("beforeend", `<input name="${this._options.fieldName}" type="hidden" value="${payload}">`);
      if (this._userStarted) {
        this.signalFinished();
      }
    }
    onWorkProgress(percent) {
      if (this._state !== STATE_IN_PROGRESS) {
        console.warn(`[privatecaptcha] skipping progress update. state=${this._state}`);
        return;
      }
      this.trace(`progress changed. percent=${percent}`);
      this.setProgress(percent);
    }
    // this updates the "UI" state of the widget
    setProgressState(state) {
      const pcElement = this._element.querySelector("private-captcha");
      if (pcElement) {
        pcElement.setState(state);
      } else {
        console.error("[privatecaptcha] component not found when changing state");
      }
    }
    // this updates the "internal" (actual) state
    setState(state) {
      this.trace(`change state. old=${this._state} new=${state}`);
      this._state = state;
      if (this._options.debug) {
        const pcElement = this._element.querySelector("private-captcha");
        if (pcElement) {
          pcElement.setDebugState(state);
        }
      }
    }
    setProgress(progress) {
      this._lastProgress = progress;
      if (STATE_IN_PROGRESS == this._state || STATE_VERIFIED == this._state) {
        const pcElement = this._element.querySelector("private-captcha");
        if (pcElement) {
          pcElement.setProgress(progress);
        } else {
          console.error("[privatecaptcha] component not found when updating progress");
        }
      }
    }
    trace(str) {
      if (this._options.debug) {
        console.debug("[privatecaptcha]", str);
      }
    }
  };

  // js/captcha.js
  window.privateCaptcha = {
    setup: setupPrivateCaptcha
    // just a class declaration
    //CaptchaWidget: CaptchaWidget,
  };
  function findCaptchaElements() {
    const elements = document.querySelectorAll(".private-captcha");
    if (elements.length === 0) {
      console.warn("PrivateCaptcha: No div was found with .private-captcha class");
    }
    return elements;
  }
  function setupPrivateCaptcha() {
    let autoWidget = window.privateCaptcha.autoWidget;
    const elements = findCaptchaElements();
    for (let htmlElement of elements) {
      if (htmlElement && !htmlElement.dataset["attached"]) {
        autoWidget = new CaptchaWidget(htmlElement);
        htmlElement.dataset["attached"] = "1";
      }
    }
    window.privateCaptcha.autoWidget = autoWidget;
  }
  if (document.readyState !== "loading") {
    setupPrivateCaptcha();
  } else {
    document.addEventListener("DOMContentLoaded", setupPrivateCaptcha);
  }
})();
//# sourceMappingURL=privatecaptcha.js.map
