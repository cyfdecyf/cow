import os
import subprocess

directlist = open('direct').read().splitlines()

direct_cn = open('direct_cn', 'w')
direct_ok = open('direct_ok', 'w')
direct_fail = open('direct_fail', 'w')


for domain in directlist:
    if domain.endswith('.cn'):
        direct_cn.write(domain + '\n')
        print domain + ': cn'
    else:
        p = subprocess.Popen(['ping', domain], stdout=subprocess.PIPE)
        streamdata = p.communicate()[0]
        ret = p.returncode
        #ret = os.system('ping {}'.format(domain))
        if ret == 0:
            direct_ok.write(domain + '\n')
            print domain + ': ok'
        else:
            direct_fail.write(domain + '\n')
            print domain + ': fail'
            
direct_cn.close()
direct_ok.close()
direct_fail.close()
