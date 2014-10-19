import os

directlist = open('direct').read().splitlines()

with open('direct_other', 'a') as outfile:
    for domain in directlist:
        if not domain.endswith('.cn'):
            ret = os.system('ping {}'.format(domain))
            print ret
            
with open('direct_out', 'a') as outfile:
    for domain in directlist:
        if domain.endswith('.cn'):
            print domain
            outfile.write(domain + '\n')
            
