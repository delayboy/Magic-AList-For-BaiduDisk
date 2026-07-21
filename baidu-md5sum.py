#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""
百度网盘特制MD5算法命令行工具

百度网盘对文件的MD5值做了一套自定义的加密变换(encrypt_md5),
导致标准md5sum算出的值与百度网盘API返回的MD5不一致。

本工具可以:
  1. 计算文件的"百度加密MD5"(encrypt_md5) — 与百度API返回的md5字段匹配
  2. 计算文件的"百度切片MD5"(slice_md5) — 前256KB的MD5, 用于秒传
  3. 计算文件的标准MD5 — 原始hexdigest
  4. 将标准MD5转换为百度加密MD5 (encrypt)
  5. 将百度加密MD5还原为标准MD5 (decrypt)

算法来源: bypy项目 cached.py 中的 encrypt_md5() 函数
  commit 47466d3 — PR #576 添加了 encrypt_md5() 函数
  commit 072e59e — PR #530 曾用 block_list[0] 替代 f['md5'], 后被回退

用法示例:
  baidu-md5sum.py <文件>              # 默认: 输出百度加密MD5
  baidu-md5sum.py -a <文件>           # 输出所有MD5 (标准/加密/切片)
  baidu-md5sum.py -e <标准MD5字符串>  # 将标准MD5转为百度加密MD5
  baidu-md5sum.py -d <百度加密MD5>    # 将百度加密MD5还原为标准MD5
"""

from __future__ import print_function
import hashlib
import sys
import os


def standard_md5(filepath):
    """计算文件的标准MD5 (原始hexdigest)"""
    m = hashlib.md5()
    with open(filepath, 'rb') as f:
        while True:
            buf = f.read(1024 * 1024)
            if buf:
                m.update(buf)
            else:
                break
    return m.hexdigest()


def slice_md5(filepath):
    """计算百度切片MD5 — 文件前256KB的标准MD5, 用于秒传接口"""
    m = hashlib.md5()
    with open(filepath, 'rb') as f:
        buf = f.read(256 * 1024)
        m.update(buf)
    return m.hexdigest()


def encrypt_md5(md5str):
    """
    百度网盘特制MD5加密算法

    步骤:
      1. 字节序交换: [0:32] -> [8:16]+[0:8]+[24:32]+[16:24]
         即把32字符MD5分为4段(每段8字符), 交换顺序: (2,1,4,3)
      2. XOR混淆: 对每个字符, hex_digit ^ (15 & position_index)
      3. 位置9替换: 将第10个字符从hex数字映射为字母 g~v (0->g, 1->h, ...)

    算法完全可逆, 见 decrypt_md5()
    """
    if len(md5str) != 32:
        return md5str

    # 验证输入是否为合法的32位hex字符串
    for i in range(32):
        v = int(md5str[i], 16)
        if v < 0 or v > 15:
            return md5str

    # 步骤1: 字节序交换
    md5str = md5str[8:16] + md5str[0:8] + md5str[24:32] + md5str[16:24]

    # 步骤2: XOR混淆
    encryptstr = ''
    for e in range(len(md5str)):
        encryptstr += hex(int(md5str[e], 16) ^ (15 & e))[2:3]

    # 步骤3: 位置9替换 (hex数字 -> 字母 g~v)
    return encryptstr[0:9] + chr(ord('g') + int(encryptstr[9], 16)) + encryptstr[10:]


def decrypt_md5(encrypted_str):
    """
    百度加密MD5还原算法 (encrypt_md5的逆运算)

    步骤:
      1. 位置9还原: 如果第10个字符在 g~v 范围, 映射回hex数字 (g->0, h->1, ...)
      2. XOR还原: XOR是自逆运算, 再次执行 hex_digit ^ (15 & position_index)
      3. 字节序还原: 将交换的四段按逆序恢复
         encrypted = [8:16]+[0:8]+[24:32]+[16:24]
         原始第1段 = encrypted[8:16] (原[0:8]在位置8-15)
         原始第2段 = encrypted[0:8]  (原[8:16]在位置0-7)
         原始第3段 = encrypted[16:24] (原[16:24]在位置16-23)
         原始第4段 = encrypted[24:32] (原[24:32]在位置24-31)
         所以还原: encrypted[0:8]+encrypted[8:16]+encrypted[16:24]+encrypted[24:32] -> wait, that's wrong

    实际上:
      encrypt交换: orig[0:8] -> pos[8:16], orig[8:16] -> pos[0:8], orig[16:24] -> pos[24:32], orig[24:32] -> pos[16:24]
      encrypted = orig[8:16] + orig[0:8] + orig[24:32] + orig[16:24]
      所以: orig = encrypted[8:16] + encrypted[0:8] + encrypted[16:24] + encrypted[24:32]
    """
    if len(encrypted_str) != 32:
        return encrypted_str

    # 步骤1逆: 位置9还原 (字母 g~v -> hex数字)
    char9 = encrypted_str[9]
    if 'g' <= char9 <= 'v':
        digit9 = hex(ord(char9) - ord('g'))[2:3]
    else:
        digit9 = char9  # 如果不是字母范围, 保持原样

    encryptstr = encrypted_str[0:9] + digit9 + encrypted_str[10:]

    # 步骤2逆: XOR还原 (XOR是自逆运算, 再次 XOR 即还原)
    decrypted = ''
    for e in range(len(encryptstr)):
        decrypted += hex(int(encryptstr[e], 16) ^ (15 & e))[2:3]

    # 步骤3逆: 字节序还原
    # encrypted的排列: pos[0:8]=orig[8:16], pos[8:16]=orig[0:8],
    #                   pos[16:24]=orig[24:32], pos[24:32]=orig[16:24]
    # 还原: orig = pos[8:16]+pos[0:8]+pos[16:24]+pos[24:32]... 不对
    # 还原应该是: orig[0:8]=pos[8:16], orig[8:16]=pos[0:8],
    #             orig[16:24]=pos[24:32], orig[24:32]=pos[16:24]
    # 所以 orig = pos[8:16] + pos[0:8] + pos[24:32] + pos[16:24]
    # 但这跟encrypt一模一样! 因为交换是 (2,1,4,3), 做两次就还原了
    # 不对, 仔细想:
    # encrypt: new = orig[8:16]+orig[0:8]+orig[24:32]+orig[16:24]
    # 这是交换 (0,1) 和 (2,3) 两对, 所以对 new 再做一次交换就还原:
    # orig = new[8:16]+new[0:8]+new[24:32]+new[16:24]
    # 即 orig = decrypted[8:16]+decrypted[0:8]+decrypted[24:32]+decrypted[16:24]
    result = decrypted[8:16] + decrypted[0:8] + decrypted[24:32] + decrypted[16:24]

    return result


def main():
    args = sys.argv[1:]

    if not args:
        print(__doc__)
        sys.exit(0)

    mode = 'file'  # default: compute file's baidu-md5

    if args[0] == '-a':
        mode = 'all'
        args = args[1:]
    elif args[0] == '-e':
        mode = 'encrypt'
        args = args[1:]
    elif args[0] == '-d':
        mode = 'decrypt'
        args = args[1:]
    elif args[0] == '-s':
        mode = 'slice'
        args = args[1:]
    elif args[0] == '-r':
        mode = 'raw'
        args = args[1:]

    if not args:
        print("错误: 缺少参数", file=sys.stderr)
        sys.exit(1)

    if mode == 'encrypt':
        # 将标准MD5字符串转为百度加密MD5
        md5str = args[0].lower().strip()
        if len(md5str) != 32:
            print("错误: 输入必须是32字符的MD5 hex字符串", file=sys.stderr)
            sys.exit(1)
        print(f"标准MD5:   {md5str}")
        print(f"百度加密:  {encrypt_md5(md5str)}")

    elif mode == 'decrypt':
        # 将百度加密MD5还原为标准MD5
        encstr = args[0].lower().strip()
        if len(encstr) != 32:
            print("错误: 输入必须是32字符的加密MD5字符串", file=sys.stderr)
            sys.exit(1)
        print(f"百度加密:  {encstr}")
        print(f"标准MD5:   {decrypt_md5(encstr)}")

    elif mode in ('file', 'all', 'slice', 'raw'):
        # 计算文件的MD5
        for filepath in args:
            if not os.path.isfile(filepath):
                print(f"错误: '{filepath}' 不是有效的文件", file=sys.stderr)
                continue

            if mode == 'all':
                raw = standard_md5(filepath)
                enc = encrypt_md5(raw)
                slc = slice_md5(filepath)
                size = os.path.getsize(filepath)
                print(f"文件: {filepath}  ({size} bytes)")
                print(f"  标准MD5:      {raw}")
                print(f"  百度加密MD5:  {enc}")
                print(f"  切片MD5:      {slc}  (前256KB)")
            elif mode == 'file':
                raw = standard_md5(filepath)
                enc = encrypt_md5(raw)
                print(f"{enc}  {filepath}")
            elif mode == 'slice':
                slc = slice_md5(filepath)
                print(f"{slc}  {filepath}  (前256KB)")
            elif mode == 'raw':
                raw = standard_md5(filepath)
                print(f"{raw}  {filepath}")


if __name__ == '__main__':
    main()
