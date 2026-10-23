#!/usr/bin/env python3
"""Create one colleague's independent workspace through the local signup API."""
import json
import secrets
import urllib.error
import urllib.request


def main():
    name = input('同事姓名：').strip()
    email = input('同事登录邮箱：').strip()
    if not name or '@' not in email or any(c.isspace() for c in email):
        raise SystemExit('姓名或邮箱格式不正确；未创建账号。')
    password = secrets.token_urlsafe(24)
    request = urllib.request.Request('http://127.0.0.1:8090/api/v1/auth/register',
        data=json.dumps({'username': name, 'email': email, 'password': password}, ensure_ascii=False).encode(),
        headers={'Content-Type': 'application/json'}, method='POST')
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            result = json.load(response)
            if not result.get('success'):
                raise SystemExit('账号创建未成功，请在本机平台检查；未修改已有账号。')
    except urllib.error.HTTPError as error:
        raise SystemExit(f'创建未完成（HTTP {error.code}）。若邮箱已存在，请使用原账号；不会重置原密码。')
    print('\n已创建独立账号与个人空间。请由你将以下信息私下交给这位同事：')
    print('登录邮箱：' + email)
    print('初始密码：' + password)
    print('访问链接：双击“查看公网地址.command”获取当前地址。')
    print('同事登录后，可在“共享空间”加入你的项目；请勿共用同一账号。')
    print('此脚本不发送邮件、不保存密码文件、不修改原有资料。')


if __name__ == '__main__':
    main()
